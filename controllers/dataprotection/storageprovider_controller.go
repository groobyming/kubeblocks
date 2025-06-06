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

package dataprotection

import (
	"context"
	"slices"
	"sync"

	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dpv1alpha1 "github.com/apecloud/kubeblocks/apis/dataprotection/v1alpha1"
	"github.com/apecloud/kubeblocks/pkg/controller/multicluster"
	intctrlutil "github.com/apecloud/kubeblocks/pkg/controllerutil"
	dptypes "github.com/apecloud/kubeblocks/pkg/dataprotection/types"
)

const (
	// event reasons
	CSIDriverObjectFound = "CSIDriverObjectFound"
	CheckCSIDriverFailed = "CheckCSIDriverFailed"
)

// StorageProviderReconciler reconciles a StorageProvider object
type StorageProviderReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	MultiClusterMgr multicluster.Manager

	mu                 sync.Mutex
	driverDependencies map[string][]string // driver name => list of provider names
}

// +kubebuilder:rbac:groups=dataprotection.kubeblocks.io,resources=storageproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dataprotection.kubeblocks.io,resources=storageproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dataprotection.kubeblocks.io,resources=storageproviders/finalizers,verbs=update

// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *StorageProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("storageprovider", req.NamespacedName)
	reqCtx := intctrlutil.RequestCtx{
		Ctx:      ctx,
		Req:      req,
		Log:      logger,
		Recorder: r.Recorder,
	}

	// get provider object
	provider := &dpv1alpha1.StorageProvider{}
	if err := r.Get(ctx, req.NamespacedName, provider); err != nil {
		if apierrors.IsNotFound(err) {
			r.removeDependency(req.Name)
			return intctrlutil.Reconciled()
		}
		return intctrlutil.CheckedRequeueWithError(err, reqCtx.Log, "failed to get StorageProvider")
	}

	// add dependency to CSIDriver
	r.ensureDependency(provider)

	// We should not add a finalizer to the StorageProvider resource,
	// because when KubeBlocks is uninstalled, the controller workload will be stopped first,
	// so the finalizer will not be processed, preventing those StorageProvider resources
	// from being deleted.
	// Remove the finalizer if it's present.
	if err := r.removeFinalizer(reqCtx, provider); err != nil {
		return intctrlutil.CheckedRequeueWithError(err, reqCtx.Log, "failed to remove finalizer")
	}

	// check CSI driver if specified
	if provider.Spec.CSIDriverName != "" {
		err := r.checkCSIDriver(reqCtx, provider.Spec.CSIDriverName)
		if err != nil {
			// update status for the CSI driver check
			if updateStatusErr := r.updateStatus(reqCtx, provider, err); updateStatusErr != nil {
				return intctrlutil.CheckedRequeueWithError(updateStatusErr, reqCtx.Log,
					"failed to update status")
			}
			return intctrlutil.CheckedRequeueWithError(err, reqCtx.Log,
				"failed to check CSIDriver %s", provider.Spec.CSIDriverName)
		}
	}

	// update status
	if updateStatusErr := r.updateStatus(reqCtx, provider, nil); updateStatusErr != nil {
		return intctrlutil.CheckedRequeueWithError(updateStatusErr, reqCtx.Log,
			"failed to update status")
	}

	return intctrlutil.Reconciled()
}

func (r *StorageProviderReconciler) removeFinalizer(reqCtx intctrlutil.RequestCtx,
	provider *dpv1alpha1.StorageProvider) error {
	pos := slices.Index(provider.Finalizers, dptypes.DataProtectionFinalizerName)
	if pos < 0 {
		return nil
	}
	provider.Finalizers = slices.Delete(provider.Finalizers, pos, pos+1)
	if err := r.Update(reqCtx.Ctx, provider); err != nil {
		return err
	}
	return nil
}

func (r *StorageProviderReconciler) updateStatus(reqCtx intctrlutil.RequestCtx,
	provider *dpv1alpha1.StorageProvider,
	checkErr error) error {
	var phase dpv1alpha1.StorageProviderPhase
	var cond metav1.Condition
	if checkErr == nil {
		phase = dpv1alpha1.StorageProviderReady
		cond = metav1.Condition{
			Type:               dpv1alpha1.ConditionTypeCSIDriverInstalled,
			Status:             metav1.ConditionTrue,
			Reason:             CSIDriverObjectFound,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: provider.Generation,
		}
	} else {
		phase = dpv1alpha1.StorageProviderNotReady
		cond = metav1.Condition{
			Type:               dpv1alpha1.ConditionTypeCSIDriverInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             CheckCSIDriverFailed,
			Message:            checkErr.Error(),
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: provider.Generation,
		}
	}

	if phase == provider.Status.Phase {
		return nil
	}
	patch := client.MergeFrom(provider.DeepCopy())
	provider.Status.Phase = phase
	meta.SetStatusCondition(&provider.Status.Conditions, cond)
	return r.Client.Status().Patch(reqCtx.Ctx, provider, patch)
}

func (r *StorageProviderReconciler) checkCSIDriver(reqCtx intctrlutil.RequestCtx, driverName string) error {
	if r.MultiClusterMgr != nil {
		if err := r.checkCSIDriverMultiCluster(reqCtx, driverName); err != nil {
			return err
		}
	}
	// check existence of CSIDriver in the control cluster
	return r.Client.Get(reqCtx.Ctx, client.ObjectKey{Name: driverName}, &storagev1.CSIDriver{},
		multicluster.InControlContext())
}

func (r *StorageProviderReconciler) checkCSIDriverMultiCluster(reqCtx intctrlutil.RequestCtx, driverName string) error {
	objKey := client.ObjectKey{Name: driverName}
	for _, cluster := range r.MultiClusterMgr.GetContexts() {
		driver := &storagev1.CSIDriver{}
		getCtx := multicluster.IntoContext(reqCtx.Ctx, cluster)
		err := r.Get(getCtx, objKey, driver, multicluster.Oneshot())
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *StorageProviderReconciler) ensureDependency(provider *dpv1alpha1.StorageProvider) {
	if provider.Spec.CSIDriverName == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.driverDependencies == nil {
		r.driverDependencies = make(map[string][]string)
	}
	driverName := provider.Spec.CSIDriverName
	list := r.driverDependencies[driverName]
	for _, x := range list {
		if x == provider.Name {
			return
		}
	}
	r.driverDependencies[driverName] = append(list, provider.Name)
}

func (r *StorageProviderReconciler) removeDependency(providerName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for driverName := range r.driverDependencies {
		list := r.driverDependencies[driverName]
		for i, x := range list {
			if x == providerName {
				list[i] = list[len(list)-1]
				r.driverDependencies[driverName] = list[:len(list)-1]
				break
			}
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := intctrlutil.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.StorageProvider{})

	mapCSIDriverToProvider := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
		r.mu.Lock()
		defer r.mu.Unlock()
		driverName := object.GetName()
		list := r.driverDependencies[driverName]
		var ret []reconcile.Request
		for _, x := range list {
			ret = append(ret, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Name: x,
				},
			})
		}
		return ret
	})
	b = b.Watches(&storagev1.CSIDriver{}, mapCSIDriverToProvider)
	if r.MultiClusterMgr != nil {
		r.MultiClusterMgr.Watch(b, &storagev1.CSIDriver{}, mapCSIDriverToProvider)
	}

	return b.Complete(r)
}
