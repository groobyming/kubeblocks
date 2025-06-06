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

package operations

import (
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1 "github.com/apecloud/kubeblocks/apis/apps/v1"
	dpv1alpha1 "github.com/apecloud/kubeblocks/apis/dataprotection/v1alpha1"
	opsv1alpha1 "github.com/apecloud/kubeblocks/apis/operations/v1alpha1"
	"github.com/apecloud/kubeblocks/pkg/constant"
	intctrlutil "github.com/apecloud/kubeblocks/pkg/controllerutil"
	"github.com/apecloud/kubeblocks/pkg/dataprotection/restore"
	dptypes "github.com/apecloud/kubeblocks/pkg/dataprotection/types"
	"github.com/apecloud/kubeblocks/pkg/operations/util"
)

type RestoreOpsHandler struct{}

var _ OpsHandler = RestoreOpsHandler{}

func init() {
	// register restore operation, it will create a new cluster
	// so set IsClusterCreationEnabled to true
	restoreBehaviour := OpsBehaviour{
		OpsHandler:        RestoreOpsHandler{},
		IsClusterCreation: true,
	}

	opsMgr := GetOpsManager()
	opsMgr.RegisterOps(opsv1alpha1.RestoreType, restoreBehaviour)
}

// ActionStartedCondition the started condition when handling the restore request.
func (r RestoreOpsHandler) ActionStartedCondition(reqCtx intctrlutil.RequestCtx, cli client.Client, opsRes *OpsResource) (*metav1.Condition, error) {
	return opsv1alpha1.NewRestoreCondition(opsRes.OpsRequest), nil
}

// Action implements the restore action.
func (r RestoreOpsHandler) Action(reqCtx intctrlutil.RequestCtx, cli client.Client, opsRes *OpsResource) error {
	var cluster *appsv1.Cluster
	var err error

	opsRequest := opsRes.OpsRequest

	// restore the cluster from the backup
	if cluster, err = r.restoreClusterFromBackup(reqCtx, cli, opsRequest); err != nil {
		return err
	}

	// create cluster
	if err = cli.Create(reqCtx.Ctx, cluster); err != nil {
		if apierrors.IsAlreadyExists(err) && opsRequest.Labels[constant.AppInstanceLabelKey] != "" {
			// already create by this opsRequest
			return nil
		}
		return err
	}
	opsRes.Cluster = cluster

	// add labels of clusterRef and type to OpsRequest
	// and set owner reference to cluster
	patch := client.MergeFrom(opsRequest.DeepCopy())
	if opsRequest.Labels == nil {
		opsRequest.Labels = make(map[string]string)
	}
	opsRequest.Labels[constant.AppInstanceLabelKey] = opsRequest.Spec.GetClusterName()
	opsRequest.Labels[constant.OpsRequestTypeLabelKey] = string(opsRequest.Spec.Type)
	scheme, _ := appsv1.SchemeBuilder.Build()
	if err = controllerutil.SetOwnerReference(cluster, opsRequest, scheme); err != nil {
		return err
	}
	if err = cli.Patch(reqCtx.Ctx, opsRequest, patch); err != nil {
		return err
	}
	return nil
}

// ReconcileAction implements the restore action.
// It will check the cluster status and update the OpsRequest status.
// If the cluster is running, it will update the OpsRequest status to Complete.
// If the cluster is failed, it will update the OpsRequest status to Failed.
// If the cluster is not running, it will update the OpsRequest status to Running.
func (r RestoreOpsHandler) ReconcileAction(reqCtx intctrlutil.RequestCtx, cli client.Client, opsRes *OpsResource) (opsv1alpha1.OpsPhase, time.Duration, error) {
	opsRequest := opsRes.OpsRequest
	clusterDef := opsRequest.Spec.GetClusterName()

	// get cluster
	cluster := &appsv1.Cluster{}
	if err := cli.Get(reqCtx.Ctx, client.ObjectKey{
		Namespace: opsRequest.GetNamespace(),
		Name:      clusterDef,
	}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			_ = PatchClusterNotFound(reqCtx.Ctx, cli, opsRes)
		}
		return opsv1alpha1.OpsFailedPhase, 0, err
	}
	opsRes.Cluster = cluster
	// check if the cluster is running
	if cluster.Status.Phase == appsv1.RunningClusterPhase {
		return opsv1alpha1.OpsSucceedPhase, 0, nil
	} else if cluster.Status.Phase == appsv1.FailedClusterPhase || cluster.IsDeleting() {
		return opsv1alpha1.OpsFailedPhase, 0, fmt.Errorf("restore failed")
	}
	return opsv1alpha1.OpsRunningPhase, 0, nil
}

// SaveLastConfiguration saves last configuration to the OpsRequest.status.lastConfiguration
func (r RestoreOpsHandler) SaveLastConfiguration(reqCtx intctrlutil.RequestCtx, cli client.Client, opsResource *OpsResource) error {
	return nil
}

func (r RestoreOpsHandler) restoreClusterFromBackup(reqCtx intctrlutil.RequestCtx, cli client.Client, opsRequest *opsv1alpha1.OpsRequest) (*appsv1.Cluster, error) {
	restoreSpec := opsRequest.Spec.GetRestore()
	if restoreSpec == nil {
		return nil, intctrlutil.NewFatalError("spec.restore can not be empty")
	}
	backupName := restoreSpec.BackupName
	backupNamespace := restoreSpec.BackupNamespace
	if backupNamespace == "" {
		backupNamespace = opsRequest.Namespace
	}
	// check if the backup exists
	backup := &dpv1alpha1.Backup{}
	if err := cli.Get(reqCtx.Ctx, client.ObjectKey{
		Name:      backupName,
		Namespace: backupNamespace,
	}, backup); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, intctrlutil.NewFatalError(fmt.Sprintf("backup %s not found in namespace %s", backupName, backupNamespace))
		}
		return nil, err
	}

	// check if the backup is completed
	backupType := backup.Labels[dptypes.BackupTypeLabelKey]
	if backup.Status.Phase != dpv1alpha1.BackupPhaseCompleted && backupType != string(dpv1alpha1.BackupTypeContinuous) {
		return nil, intctrlutil.NewFatalError(fmt.Sprintf("backup %s status is %s, only completed backup can be used to restore", backupName, backup.Status.Phase))
	}

	// format and validate the restore time
	if backupType == string(dpv1alpha1.BackupTypeContinuous) {
		restoreTimeStr, err := restore.FormatRestoreTimeAndValidate(restoreSpec.RestorePointInTime, backup)
		if err != nil {
			return nil, err
		}
		opsRequest.Spec.GetRestore().RestorePointInTime = restoreTimeStr
	}
	// get the cluster object from backup
	clusterObj, err := r.getClusterObjFromBackup(backup, opsRequest)
	if err != nil {
		return nil, err
	}
	opsRequestSlice := []opsv1alpha1.OpsRecorder{
		{
			Name: opsRequest.Name,
			Type: opsRequest.Spec.Type,
		},
	}
	util.SetOpsRequestToCluster(clusterObj, opsRequestSlice)
	return clusterObj, nil
}

func (r RestoreOpsHandler) getClusterObjFromBackup(backup *dpv1alpha1.Backup, opsRequest *opsv1alpha1.OpsRequest) (*appsv1.Cluster, error) {
	cluster := &appsv1.Cluster{}
	// use the cluster snapshot to restore firstly
	clusterString, ok := backup.Annotations[constant.ClusterSnapshotAnnotationKey]
	if !ok {
		return nil, intctrlutil.NewFatalError(fmt.Sprintf("missing snapshot annotation in backup %s, %s is empty in Annotations", backup.Name, constant.ClusterSnapshotAnnotationKey))
	}
	if err := json.Unmarshal([]byte(clusterString), &cluster); err != nil {
		return nil, err
	}
	restoreSpec := opsRequest.Spec.GetRestore()
	// set the restore annotation to cluster
	restoreAnnotation, err := restore.GetRestoreFromBackupAnnotation(backup, restoreSpec.VolumeRestorePolicy, restoreSpec.RestorePointInTime,
		restoreSpec.Env, restoreSpec.DeferPostReadyUntilClusterRunning, restoreSpec.Parameters)
	if err != nil {
		return nil, err
	}
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}
	cluster.Annotations[constant.RestoreFromBackupAnnotationKey] = restoreAnnotation
	cluster.Name = opsRequest.Spec.GetClusterName()
	cluster.Namespace = opsRequest.Namespace
	// Reset cluster services
	var services []appsv1.ClusterService
	for i := range cluster.Spec.Services {
		svc := cluster.Spec.Services[i]
		if svc.Service.Spec.Type == corev1.ServiceTypeLoadBalancer {
			continue
		}
		if svc.Service.Spec.Type == corev1.ServiceTypeNodePort {
			for j := range svc.Spec.Ports {
				svc.Spec.Ports[j].NodePort = 0
			}
		}
		if svc.Service.Spec.Selector != nil {
			delete(svc.Service.Spec.Selector, constant.AppInstanceLabelKey)
		}
		services = append(services, svc)
	}
	cluster.Spec.Services = services
	for i := range cluster.Spec.ComponentSpecs {
		cluster.Spec.ComponentSpecs[i].OfflineInstances = nil
	}
	r.rebuildShardAccountSecrets(cluster)
	r.normalizeSchedulePolicy(cluster, cluster.Spec.SchedulingPolicy)
	for i := range cluster.Spec.ComponentSpecs {
		r.normalizeSchedulePolicy(cluster, cluster.Spec.ComponentSpecs[i].SchedulingPolicy)
	}
	for i := range cluster.Spec.Shardings {
		r.normalizeSchedulePolicy(cluster, cluster.Spec.Shardings[i].Template.SchedulingPolicy)
	}
	return cluster, nil
}

// normalizeSchedulePolicy normalizes the schedule policy of the new cluster.
func (r RestoreOpsHandler) normalizeSchedulePolicy(cluster *appsv1.Cluster, schedulePolicy *appsv1.SchedulingPolicy) {
	if schedulePolicy == nil {
		return
	}
	updateLabelSelector := func(selector *metav1.LabelSelector) {
		if selector == nil {
			return
		}
		if _, ok := selector.MatchLabels[constant.AppInstanceLabelKey]; ok {
			selector.MatchLabels[constant.AppInstanceLabelKey] = cluster.Name
		}
		for i := range selector.MatchExpressions {
			matchExpression := &selector.MatchExpressions[i]
			if matchExpression.Key == constant.AppInstanceLabelKey {
				matchExpression.Values = []string{cluster.Name}
			}
		}
	}
	for i := range schedulePolicy.TopologySpreadConstraints {
		updateLabelSelector(schedulePolicy.TopologySpreadConstraints[i].LabelSelector)
	}
	if schedulePolicy.Affinity == nil {
		return
	}
	updatePodAffinityTerm := func(pats []corev1.PodAffinityTerm, wpats []corev1.WeightedPodAffinityTerm) {
		for i := range pats {
			podAffinityTerm := &pats[i]
			updateLabelSelector(podAffinityTerm.LabelSelector)
		}
		for i := range wpats {
			wpat := &wpats[i]
			updateLabelSelector(wpat.PodAffinityTerm.LabelSelector)
		}
	}
	if schedulePolicy.Affinity.PodAntiAffinity != nil {
		updatePodAffinityTerm(schedulePolicy.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			schedulePolicy.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}
	if schedulePolicy.Affinity.PodAffinity != nil {
		updatePodAffinityTerm(schedulePolicy.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			schedulePolicy.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}
}

func (r RestoreOpsHandler) rebuildShardAccountSecrets(cluster *appsv1.Cluster) {
	if len(cluster.Spec.Shardings) == 0 {
		return
	}
	for i := range cluster.Spec.Shardings {
		shardingSpec := &cluster.Spec.Shardings[i]
		template := &shardingSpec.Template
		for j := range template.SystemAccounts {
			account := &template.SystemAccounts[j]
			account.SecretRef = nil
		}
	}
}
