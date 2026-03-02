package controller

import (
	"crypto/tls"
	"sync"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// k8sState represents the state of kubernetes objects in the cluster. Methods of this struct
// are expected to acquire lock when necessary.
type k8sState struct {
	sync.RWMutex

	config *config.ControllerConf

	controllerName    string
	controllerClasses map[string]*netv1.IngressClass

	ingresses map[types.NamespacedName]*IngressEntry

	tcpProxies []*proxy.TcpProxyConfig

	ingressLoadBalancerStatus *netv1.IngressLoadBalancerStatus
	defaultTlsSecret          *tls.Certificate
}

func isIngressRelevant(s *k8sState, ingress *netv1.Ingress) bool {
	ingressClass := ingress.Spec.IngressClassName
	if ingressClass == nil {
		return false
	}

	class, ok := s.controllerClasses[*ingressClass]
	return ok && class.Spec.Controller == s.controllerName
}

func (s *k8sState) isIngressRelevant(ingress *netv1.Ingress) bool {
	s.RLock()
	defer s.RUnlock()

	return isIngressRelevant(s, ingress)
}

func objectMetaToNamespaced(obj metav1.Object) types.NamespacedName {
	return types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
}

func (s *k8sState) updateIngress(ingressConfig *IngressEntry) {
	s.Lock()
	defer s.Unlock()

	namespaced := objectMetaToNamespaced(ingressConfig.Ingress)
	s.ingresses[namespaced] = ingressConfig
}

func (s *k8sState) markIngressIrrelevant(ingress *netv1.Ingress) bool {
	s.Lock()
	defer s.Unlock()

	namespaced := objectMetaToNamespaced(ingress)
	entry, ok := s.ingresses[namespaced]
	if !ok {
		entry = &IngressEntry{
			Ingress: ingress,
		}
		s.ingresses[namespaced] = entry
		return false
	}

	wasRelevant := isIngressRelevant(s, entry.Ingress)

	entry.Ingress = ingress
	entry.Configuration = nil

	return wasRelevant
}

func (s *k8sState) removeIngress(namespaced types.NamespacedName) bool {
	s.Lock()
	defer s.Unlock()

	entry, ok := s.ingresses[namespaced]
	if !ok {
		return false
	}
	delete(s.ingresses, namespaced)

	return isIngressRelevant(s, entry.Ingress)
}
