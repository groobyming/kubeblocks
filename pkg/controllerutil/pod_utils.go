/*
Copyright (C) 2022-2025 ApeCloud Co., Ltd

This file is part of KubeBlocks project

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package controllerutil

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metautil "k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/kubectl/pkg/util/podutils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "github.com/apecloud/kubeblocks/apis/apps/v1alpha1"
	"github.com/apecloud/kubeblocks/pkg/constant"
	viper "github.com/apecloud/kubeblocks/pkg/viperx"
)

const (
	// PodContainerFailedTimeout the timeout for container of pod failures, the component phase will be set to Failed after this time.
	PodContainerFailedTimeout = 10 * time.Second
)

// GetContainerByConfigSpec searches for container using the configmap of config from the pod
//
//	e.g.:
//	ClusterDefinition.configTemplateRef:
//		 - Name: "mysql-8.0"
//		   VolumeName: "mysql_config"
//
//
//	PodTemplate.containers[*].volumeMounts:
//		 - mountPath: /data/config
//		   name: mysql_config
//		 - mountPath: /data
//		   name: data
//		 - mountPath: /log
//		   name: log
func GetContainerByConfigSpec(podSpec *corev1.PodSpec, configs []appsv1alpha1.ComponentConfigSpec) *corev1.Container {
	containers := podSpec.Containers
	initContainers := podSpec.InitContainers
	if container := getContainerWithTplList(containers, configs); container != nil {
		return container
	}
	if container := getContainerWithTplList(initContainers, configs); container != nil {
		return container
	}
	return nil
}

// GetPodContainerWithVolumeMount searches for containers mounting the volume
func GetPodContainerWithVolumeMount(podSpec *corev1.PodSpec, volumeName string) []*corev1.Container {
	containers := podSpec.Containers
	if len(containers) == 0 || volumeName == "" {
		return nil
	}
	return getContainerWithVolumeMount(containers, volumeName)
}

// GetVolumeMountName finds the volume with mount name
func GetVolumeMountName(volumes []corev1.Volume, resourceName string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].ConfigMap != nil && volumes[i].ConfigMap.Name == resourceName {
			return &volumes[i]
		}
		if volumes[i].Projected == nil {
			continue
		}
		for j := range volumes[i].Projected.Sources {
			if volumes[i].Projected.Sources[j].ConfigMap != nil && volumes[i].Projected.Sources[j].ConfigMap.Name == resourceName {
				return &volumes[i]
			}
		}
	}
	return nil
}

type containerNameFilter func(containerName string) bool

func GetContainersByConfigmap(containers []corev1.Container, volumeName string, cmName string, filters ...containerNameFilter) []string {
	containerFilter := func(c corev1.Container) bool {
		for _, f := range filters {
			if (len(c.VolumeMounts) == 0 && len(c.EnvFrom) == 0) ||
				f(c.Name) {
				return true
			}
		}
		return false
	}

	tmpList := make([]string, 0, len(containers))
	for _, c := range containers {
		if containerFilter(c) {
			continue
		}
		for _, vm := range c.VolumeMounts {
			if vm.Name == volumeName {
				tmpList = append(tmpList, c.Name)
				goto breakHere
			}
		}
		if cmName == "" {
			continue
		}
		for _, source := range c.EnvFrom {
			if source.ConfigMapRef != nil && source.ConfigMapRef.Name == cmName {
				tmpList = append(tmpList, c.Name)
				break
			}
		}
	breakHere:
	}
	return tmpList
}

func getContainerWithTplList(containers []corev1.Container, configs []appsv1alpha1.ComponentConfigSpec) *corev1.Container {
	if len(containers) == 0 {
		return nil
	}
	for i, c := range containers {
		volumeMounts := c.VolumeMounts
		if len(volumeMounts) > 0 && checkContainerWithVolumeMount(volumeMounts, configs) {
			return &containers[i]
		}
	}
	return nil
}

func checkContainerWithVolumeMount(volumeMounts []corev1.VolumeMount, configs []appsv1alpha1.ComponentConfigSpec) bool {
	volumes := make(map[string]int)
	for _, c := range configs {
		for j, vm := range volumeMounts {
			if vm.Name == c.VolumeName {
				volumes[vm.Name] = j
				break
			}
		}
	}
	return len(configs) == len(volumes)
}

func getContainerWithVolumeMount(containers []corev1.Container, volumeName string) []*corev1.Container {
	mountContainers := make([]*corev1.Container, 0, len(containers))
	for i, c := range containers {
		volumeMounts := c.VolumeMounts
		for _, vm := range volumeMounts {
			if vm.Name == volumeName {
				mountContainers = append(mountContainers, &containers[i])
				break
			}
		}
	}
	return mountContainers
}

func GetVolumeMountByVolume(container *corev1.Container, volumeName string) *corev1.VolumeMount {
	for _, volume := range container.VolumeMounts {
		if volume.Name == volumeName {
			return &volume
		}
	}

	return nil
}

// GetCoreNum gets content of Resources.Limits.cpu
func GetCoreNum(container corev1.Container) int64 {
	limits := container.Resources.Limits
	if val, ok := (limits)[corev1.ResourceCPU]; ok {
		return val.Value()
	}
	return 0
}

// GetMemorySize gets content of Resources.Limits.memory
func GetMemorySize(container corev1.Container) int64 {
	limits := container.Resources.Limits
	if val, ok := (limits)[corev1.ResourceMemory]; ok {
		return val.Value()
	}
	return 0
}

// GetRequestMemorySize gets content of Resources.Limits.memory
func GetRequestMemorySize(container corev1.Container) int64 {
	requests := container.Resources.Requests
	if val, ok := (requests)[corev1.ResourceMemory]; ok {
		return val.Value()
	}
	return 0
}

// GetStorageSizeFromPersistentVolume gets content of Resources.Requests.storage
func GetStorageSizeFromPersistentVolume(pvc corev1.PersistentVolumeClaimTemplate) int64 {
	requests := pvc.Spec.Resources.Requests
	if val, ok := (requests)[corev1.ResourceStorage]; ok {
		return val.Value()
	}
	return -1
}

// isTerminating returns true if pod's DeletionTimestamp has been set
func isTerminating(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

// IsPodReady returns true if pod is ready
// Currently, if pod is being deleted and have a grace period, k8s still considers it ready,
// which is not what we expect. See https://github.com/kubernetes/kubernetes/issues/129552
func IsPodReady(pod *corev1.Pod) bool {
	return podutils.IsPodReady(pod) && !isTerminating(pod)
}

// IsPodAvailable returns true if pod is ready for at least minReadySeconds
func IsPodAvailable(pod *corev1.Pod, minReadySeconds int32) bool {
	return podutils.IsPodAvailable(pod, minReadySeconds, metav1.Now()) && !isTerminating(pod)
}

// GetContainerID gets the containerID from pod by name
func GetContainerID(pod *corev1.Pod, containerName string) string {
	const containerSep = "//"

	// container id is present in the form of <runtime>://<container-id>
	// e.g: containerID: docker://27d1586d53ef9a6af5bd983831d13b6a38128119fadcdc22894d7b2397758eb5
	for _, container := range pod.Status.ContainerStatuses {
		if container.Name == containerName {
			return strings.Split(container.ContainerID, containerSep)[1]
		}
	}
	return ""
}

func GetPodCondition(status *corev1.PodStatus, conditionType corev1.PodConditionType) *corev1.PodCondition {
	if status == nil {
		return nil
	}

	if len(status.Conditions) == 0 {
		return nil
	}

	for i, condition := range status.Conditions {
		if condition.Type == conditionType {
			return &status.Conditions[i]
		}
	}
	return nil
}

func IsMatchConfigVersion(obj client.Object, labelKey string, version string) bool {
	labels := obj.GetLabels()
	if len(labels) == 0 {
		return false
	}
	if lastVersion, ok := labels[labelKey]; ok && lastVersion == version {
		return true
	}
	return false
}

func GetIntOrPercentValue(intOrStr *metautil.IntOrString) (int, bool, error) {
	if intOrStr.Type == metautil.Int {
		return intOrStr.IntValue(), false, nil
	}

	// parse string
	s := intOrStr.StrVal
	if strings.HasSuffix(s, "%") {
		s = strings.TrimSuffix(intOrStr.StrVal, "%")
	} else {
		return 0, false, fmt.Errorf("failed to parse percentage. [%s]", intOrStr.StrVal)
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false, fmt.Errorf("failed to atoi [%s], error: %v", intOrStr.StrVal, err)
	}
	return v, true, nil
}

func GetPortByName(pod corev1.Pod, cname, pname string) (int32, error) {
	for _, container := range pod.Spec.Containers {
		if container.Name == cname {
			for _, port := range container.Ports {
				if port.Name == pname {
					return port.ContainerPort, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("port %s not found", pname)
}

// PodIsReadyWithLabel checks if pod is ready for ConsensusSet/ReplicationSet component,
// it will be available when the pod is ready and labeled with role.
func PodIsReadyWithLabel(pod corev1.Pod) bool {
	if _, ok := pod.Labels[constant.RoleLabelKey]; !ok {
		return false
	}

	return IsPodReady(&pod)
}

// GetPodRevision gets the revision of Pod by inspecting the StatefulSetRevisionLabel. If pod has no revision empty
// string is returned.
func GetPodRevision(pod *corev1.Pod) string {
	if pod.Labels == nil {
		return ""
	}
	return pod.Labels[appsv1.StatefulSetRevisionLabel]
}

// ByPodName sorts a list of jobs by pod name
type ByPodName []corev1.Pod

// Len returns the length of byPodName for sort.Sort
func (c ByPodName) Len() int {
	return len(c)
}

// Swap swaps the items for sort.Sort
func (c ByPodName) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

// Less defines compare method for sort.Sort
func (c ByPodName) Less(i, j int) bool {
	return c[i].Name < c[j].Name
}

// BuildPodHostDNS builds the host dns of pod.
// ref: https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/
func BuildPodHostDNS(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	// build pod dns string
	// ref: https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/
	if pod.Spec.Subdomain != "" {
		hostDNS := []string{pod.Name}
		if pod.Spec.Hostname != "" {
			hostDNS[0] = pod.Spec.Hostname
		}
		hostDNS = append(hostDNS, pod.Spec.Subdomain)
		return strings.Join(hostDNS, ".")
	}
	return pod.Status.PodIP
}

// ResolvePodSpecDefaultFields set default value for some known fields of proto PodSpec @pobj.
func ResolvePodSpecDefaultFields(obj corev1.PodSpec, pobj *corev1.PodSpec) {
	resolveVolume := func(v corev1.Volume, vv *corev1.Volume) {
		if vv.DownwardAPI != nil && v.DownwardAPI != nil {
			for i := range vv.DownwardAPI.Items {
				vf := v.DownwardAPI.Items[i]
				if vf.FieldRef == nil {
					continue
				}
				vvf := &vv.DownwardAPI.Items[i]
				if vvf.FieldRef != nil && len(vvf.FieldRef.APIVersion) == 0 {
					vvf.FieldRef.APIVersion = vf.FieldRef.APIVersion
				}
			}
			if vv.DownwardAPI.DefaultMode == nil {
				vv.DownwardAPI.DefaultMode = v.DownwardAPI.DefaultMode
			}
		}
		if vv.ConfigMap != nil && v.ConfigMap != nil {
			if vv.ConfigMap.DefaultMode == nil {
				vv.ConfigMap.DefaultMode = v.ConfigMap.DefaultMode
			}
		}
	}
	for i := 0; i < min(len(obj.Volumes), len(pobj.Volumes)); i++ {
		resolveVolume(obj.Volumes[i], &pobj.Volumes[i])
	}
	for i := 0; i < min(len(obj.InitContainers), len(pobj.InitContainers)); i++ {
		ResolveContainerDefaultFields(obj.InitContainers[i], &pobj.InitContainers[i])
	}
	for i := 0; i < min(len(obj.Containers), len(pobj.Containers)); i++ {
		ResolveContainerDefaultFields(obj.Containers[i], &pobj.Containers[i])
	}
	if len(pobj.RestartPolicy) == 0 {
		pobj.RestartPolicy = obj.RestartPolicy
	}
	if pobj.TerminationGracePeriodSeconds == nil {
		pobj.TerminationGracePeriodSeconds = obj.TerminationGracePeriodSeconds
	}
	if len(pobj.DNSPolicy) == 0 {
		pobj.DNSPolicy = obj.DNSPolicy
	}
	if len(pobj.DeprecatedServiceAccount) == 0 {
		pobj.DeprecatedServiceAccount = obj.DeprecatedServiceAccount
	}
	if pobj.SecurityContext == nil {
		pobj.SecurityContext = obj.SecurityContext
	}
	if len(pobj.SchedulerName) == 0 {
		pobj.SchedulerName = obj.SchedulerName
	}
	if len(pobj.Tolerations) == 0 {
		pobj.Tolerations = obj.Tolerations
	}
	if pobj.Priority == nil {
		pobj.Priority = obj.Priority
	}
	if pobj.EnableServiceLinks == nil {
		pobj.EnableServiceLinks = obj.EnableServiceLinks
	}
	if pobj.PreemptionPolicy == nil {
		pobj.PreemptionPolicy = obj.PreemptionPolicy
	}
}

// ResolveContainerDefaultFields set default value for some known fields of proto Container @pcontainer.
func ResolveContainerDefaultFields(container corev1.Container, pcontainer *corev1.Container) {
	if len(pcontainer.TerminationMessagePath) == 0 {
		pcontainer.TerminationMessagePath = container.TerminationMessagePath
	}
	if len(pcontainer.TerminationMessagePolicy) == 0 {
		pcontainer.TerminationMessagePolicy = container.TerminationMessagePolicy
	}
	if len(pcontainer.ImagePullPolicy) == 0 {
		pcontainer.ImagePullPolicy = container.ImagePullPolicy
	}

	resolveContainerProbe := func(p corev1.Probe, pp *corev1.Probe) {
		if pp.TimeoutSeconds == 0 {
			pp.TimeoutSeconds = p.TimeoutSeconds
		}
		if pp.PeriodSeconds == 0 {
			pp.PeriodSeconds = p.PeriodSeconds
		}
		if pp.SuccessThreshold == 0 {
			pp.SuccessThreshold = p.SuccessThreshold
		}
		if pp.FailureThreshold == 0 {
			pp.FailureThreshold = p.FailureThreshold
		}
		if pp.HTTPGet != nil && len(pp.HTTPGet.Scheme) == 0 {
			if p.HTTPGet != nil {
				pp.HTTPGet.Scheme = p.HTTPGet.Scheme
			}
		}
	}
	if pcontainer.LivenessProbe != nil && container.LivenessProbe != nil {
		resolveContainerProbe(*container.LivenessProbe, pcontainer.LivenessProbe)
	}
	if pcontainer.ReadinessProbe != nil && container.ReadinessProbe != nil {
		resolveContainerProbe(*container.ReadinessProbe, pcontainer.ReadinessProbe)
	}
	if pcontainer.StartupProbe != nil && container.StartupProbe != nil {
		resolveContainerProbe(*container.StartupProbe, pcontainer.StartupProbe)
	}
}

// GetPodContainer gets the pod container by name. if containerName is empty, return the first container.
func GetPodContainer(pod *corev1.Pod, containerName string) *corev1.Container {
	if containerName == "" {
		return &pod.Spec.Containers[0]
	}
	for i := range pod.Spec.Containers {
		container := pod.Spec.Containers[i]
		if container.Name == containerName {
			return &container
		}
	}
	return nil
}

// IsPodFailedAndTimedOut checks if the pod is failed and timed out.
func IsPodFailedAndTimedOut(pod *corev1.Pod) (bool, bool, string) {
	initContainerFailed, message := isAnyContainerFailed(pod.Status.InitContainerStatuses)
	if initContainerFailed {
		return initContainerFailed, isContainerFailedAndTimedOut(pod, corev1.PodInitialized), message
	}
	containerFailed, message := isAnyContainerFailed(pod.Status.ContainerStatuses)
	if containerFailed {
		return containerFailed, isContainerFailedAndTimedOut(pod, corev1.ContainersReady), message
	}
	return false, false, ""
}

// IsAnyContainerFailed checks whether any container in the list is failed.
func isAnyContainerFailed(containersStatus []corev1.ContainerStatus) (bool, string) {
	for _, v := range containersStatus {
		waitingState := v.State.Waiting
		if waitingState != nil && waitingState.Message != "" {
			return true, waitingState.Message
		}
		terminatedState := v.State.Terminated
		if terminatedState != nil && terminatedState.ExitCode != 0 {
			return true, terminatedState.Message
		}
	}
	return false, ""
}

// IsContainerFailedAndTimedOut checks whether the failed container has timed out.
func isContainerFailedAndTimedOut(pod *corev1.Pod, podConditionType corev1.PodConditionType) bool {
	containerReadyCondition := GetPodCondition(&pod.Status, podConditionType)
	if containerReadyCondition == nil || containerReadyCondition.LastTransitionTime.IsZero() {
		return false
	}
	return time.Now().After(containerReadyCondition.LastTransitionTime.Add(PodContainerFailedTimeout))
}

func BuildImagePullSecrets() []corev1.LocalObjectReference {
	secrets := make([]corev1.LocalObjectReference, 0)
	secretsVal := viper.GetString(constant.KBImagePullSecrets)
	if secretsVal == "" {
		return secrets
	}

	// we already validate the value of KBImagePullSecrets when start server,
	// so we can ignore the error here
	_ = json.Unmarshal([]byte(secretsVal), &secrets)
	return secrets
}
