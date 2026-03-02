package controller

import (
	"context"
	"fmt"
	"log/slog"

	"codeberg.org/oidq/s-ingress/pkg/config"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (ic *IngressController) getUpstreamIpAddresses(ctx context.Context, conf *config.ControllerConf) *netv1.IngressLoadBalancerStatus {
	if conf.UpstreamServiceName == nil {
		return nil
	}

	service := &v1.Service{}
	err := ic.k8sClient.Get(
		ctx,
		types.NamespacedName{Name: *conf.UpstreamServiceName, Namespace: ic.envConfig.Namespace},
		service,
	)
	if err != nil {
		ic.log.Warn("error getting upstream service", slog.String("err", err.Error()))
		return nil
	}

	ing := netv1.IngressLoadBalancerStatus{}
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		ing.Ingress = append(ing.Ingress, netv1.IngressLoadBalancerIngress{
			IP:       ingress.IP,
			Hostname: ingress.Hostname,
		})
	}

	return &ing
}

func (ic *IngressController) ReconcileIngress(ctx context.Context, name types.NamespacedName, ingress *netv1.Ingress) error {
	if ingress == nil { // ingress deleted
		isRelevant := ic.k8sState.removeIngress(name)
		ic.RequestReconfigureWhenRelevant(isRelevant)
		return nil
	}

	isRelevant := ic.k8sState.isIngressRelevant(ingress)
	if !isRelevant {
		wasRelevant := ic.k8sState.markIngressIrrelevant(ingress)
		ic.RequestReconfigureWhenRelevant(wasRelevant)
		return nil
	}

	ic.log.Info("reconciling", slog.Any("ingress", objectMetaToNamespaced(ingress)))
	ingressConfig, err := ic.reconcileIngress(ctx, ingress)
	if err != nil {
		ic.eventRecorder.Eventf(ingress, nil, "Warning", "Reconcile", "ReconcileFailed", "Failed to reconcile ingress: %v", err)
		return fmt.Errorf("error reconciling ingress: %v", err)
	}

	ic.log.Info("reconciling ingress", "ingress", ingress.Name)
	ic.k8sState.updateIngress(&IngressEntry{
		Ingress:       ingress,
		Configuration: ingressConfig,
	})

	ic.RequestReconfigure()

	ic.eventRecorder.Eventf(ingress, nil, "Normal", "Reconcile", "ReconcileSucceeded", "Ingress reconciled")

	return nil
}

func (ic *IngressController) RemoveIngressClass(name string) {
	ic.k8sState.Lock()
	defer ic.k8sState.Unlock()

	if _, ok := ic.k8sState.controllerClasses[name]; ok {
		delete(ic.k8sState.controllerClasses, name)
		ic.log.Info("removing ingress class", slog.String("ingressClass", name))
		ic.RequestReconfigure()
	}
}

func (ic *IngressController) AddIngressClass(ingClass *netv1.IngressClass) {
	name := ingClass.Name

	ic.k8sState.Lock()
	if _, ok := ic.k8sState.controllerClasses[name]; !ok {
		ic.log.Info("adding ingress class", slog.String("ingressClass", name))
	} else {
		ic.log.Info("updating ingress class", slog.String("ingressClass", name))
	}
	ic.k8sState.controllerClasses[name] = ingClass
	ic.k8sState.Unlock()

	ic.RequestReconfigure()
}
