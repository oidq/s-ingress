package controller

import (
	"context"
	"slices"
	"time"

	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconcileWorkqueue = workqueue.TypedRateLimitingInterface[reconcile.Request]

func (ic *IngressController) getIngressesToRequeueOnClassChange(ctx context.Context, w reconcileWorkqueue, className string) {
	ingresses := &netv1.IngressList{}
	err := ic.k8sClient.List(ctx, ingresses)
	if err != nil {
		ic.log.Error("unable to list ingresses for ingressClass reconciliation")
		return
	}

	for _, ingress := range ingresses.Items {
		if ingress.Spec.IngressClassName == nil || *ingress.Spec.IngressClassName != className {
			continue
		}
		w.Add(reconcile.Request{NamespacedName: objectMetaToNamespaced(&ingress)})
	}
}

func (ic *IngressController) SetupWithManager(mgr ctrl.Manager, name string) error {
	ic.eventRecorder = mgr.GetEventRecorder("s-ingress-controller")

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		Watches(&netv1.IngressClass{}, ic.getIngressReconcilesForIngressClasses()).
		Watches(&v1.Secret{}, ic.getIngressReconcilesForSecrets()).
		Watches(&v1.ConfigMap{}, ic.getIngressReconcilesForConfigMaps()).
		Watches(&v1.Service{}, ic.getIngressReconcilesForServices()).
		For(&netv1.Ingress{}).
		Complete(ic)
}

func (ic *IngressController) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	ingress := &netv1.Ingress{}
	err := ic.k8sClient.Get(ctx, req.NamespacedName, ingress)
	switch {
	case errors.IsNotFound(err):
		// ingress class was removed
		ingress = nil
	case err != nil:
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	err = ic.ReconcileIngress(ctx, req.NamespacedName, ingress)
	if err != nil {
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}

	return reconcile.Result{}, nil
}

func (ic *IngressController) getIngressReconcilesForIngressClasses() handler.TypedEventHandler[client.Object, reconcile.Request] {
	return handler.TypedFuncs[client.Object, reconcile.Request]{
		CreateFunc: func(ctx context.Context, e event.TypedCreateEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			ingressClass := e.Object.(*netv1.IngressClass)
			ic.AddIngressClass(ingressClass)
			ic.getIngressesToRequeueOnClassChange(ctx, w, ingressClass.Name)
		},
		DeleteFunc: func(ctx context.Context, e event.TypedDeleteEvent[client.Object], w workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			ingressClass := e.Object.(*netv1.IngressClass)
			ic.RemoveIngressClass(ingressClass.Name)
			ic.getIngressesToRequeueOnClassChange(ctx, w, ingressClass.Name)
		},
	}
}

func (ic *IngressController) getIngressReconcilesForSecrets() handler.TypedEventHandler[client.Object, reconcile.Request] {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
		// check changes on default secret
		if ic.k8sState.config != nil {
			if object.GetName() == ic.k8sState.config.Tls.DefaultTlsSecret && object.GetNamespace() == ic.envConfig.Namespace {
				ic.RequestReconcileCommon()
				return nil
			}
		}

		// check secrets on ingres secrets
		var reconcileRequests []reconcile.Request
		namespaced := objectMetaToNamespaced(object)
		for namespacedIngress, ingress := range ic.k8sState.ingresses {
			ingressConf := ingress.Configuration
			if ingressConf != nil && slices.Contains(ingressConf.UsedSecrets, namespaced) {
				reconcileRequests = append(reconcileRequests, reconcile.Request{NamespacedName: namespacedIngress})
			}
		}

		return reconcileRequests
	})
}

func (ic *IngressController) getIngressReconcilesForConfigMaps() handler.TypedEventHandler[client.Object, reconcile.Request] {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
		// check changes on default secret
		namespacedCm := objectMetaToNamespaced(object)
		if namespacedCm == ic.envConfig.ControllerConfigMap {
			ic.RequestReconcileCommon()
			return nil
		}

		// check secrets on ingres secrets
		var reconcileRequests []reconcile.Request
		namespaced := objectMetaToNamespaced(object)
		for namespacedIngress, ingress := range ic.k8sState.ingresses {
			ingressConf := ingress.Configuration
			if ingressConf != nil && slices.Contains(ingressConf.UsedConfigMaps, namespaced) {
				reconcileRequests = append(reconcileRequests, reconcile.Request{NamespacedName: namespacedIngress})
			}
		}

		return reconcileRequests
	})
}

func (ic *IngressController) getIngressReconcilesForServices() handler.TypedEventHandler[client.Object, reconcile.Request] {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
		// check changes on upstream svc
		conf := ic.k8sState.config
		if conf != nil && conf.UpstreamServiceName != nil {
			namespacedSvc := objectMetaToNamespaced(object)
			namespacedUpstreamSvc := types.NamespacedName{Name: *ic.k8sState.config.UpstreamServiceName, Namespace: ic.envConfig.Namespace}
			if namespacedSvc == namespacedUpstreamSvc {
				ic.RequestReconcileCommon()
				return nil
			}
		}

		// check services on ingresses
		var reconcileRequests []reconcile.Request
		namespaced := objectMetaToNamespaced(object)
		for namespacedIngress, ingress := range ic.k8sState.ingresses {
			ingressConf := ingress.Configuration
			if ingressConf != nil && slices.Contains(ingressConf.UsedServices, namespaced) {
				reconcileRequests = append(reconcileRequests, reconcile.Request{NamespacedName: namespacedIngress})
			}
		}

		return reconcileRequests
	})
}
