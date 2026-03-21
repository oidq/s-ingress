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
	state := ic.k8sState
	state.Lock()
	defer state.Unlock()

	if ingress == nil { // ingress deleted
		isRelevant := state.removeIngress(name)
		ic.RequestReconfigureWhenRelevant(isRelevant)
		return nil
	}

	isRelevant := state.isIngressRelevant(ingress)
	if !isRelevant {
		wasRelevant := state.markIngressIrrelevant(ingress)
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
	state.updateIngress(&IngressEntry{
		Ingress:       ingress,
		Configuration: ingressConfig,
	})

	ic.RequestReconfigure()

	ic.eventRecorder.Eventf(ingress, nil, "Normal", "Reconcile", "ReconcileSucceeded", "Ingress reconciled")

	return nil
}

func (ic *IngressController) RemoveIngressClass(name string) {
	state := ic.k8sState
	state.Lock()
	defer state.Unlock()

	if _, ok := state.controllerClasses[name]; ok {
		delete(state.controllerClasses, name)
		ic.log.Info("removing ingress class", slog.String("ingressClass", name))
		ic.RequestReconfigure()
	}
}

func (ic *IngressController) AddIngressClass(ingClass *netv1.IngressClass) {
	state := ic.k8sState
	state.Lock()
	defer state.Unlock()

	name := ingClass.Name

	if _, ok := state.controllerClasses[name]; !ok {
		ic.log.Info("adding ingress class", slog.String("ingressClass", name))
	} else {
		ic.log.Info("updating ingress class", slog.String("ingressClass", name))
	}
	state.controllerClasses[name] = ingClass

	ic.RequestReconfigure()
}
