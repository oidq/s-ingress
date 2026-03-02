package config

import (
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ModuleCreator is a function that returns initialized [ModuleInstance] based
// on the provided config. This is the actual entrypoint of each module and is run
// with a current [*ControllerConf]. Beware that the [ModuleInstance] can be replaced
// anytime on config reconciliation.
type ModuleCreator = func(conf *ControllerConf) (ModuleInstance, error)

// ModuleInstance is an interface matching the actual module instance. All the methods
// are called in the configuration phase, only the returned functions will be run in the proxy.
// Returning the default value (nil) from the function signalizes that the module is not interested
// in this hook/middleware.
//
// All [ModuleInstance] implementations must embed [config.Module] to allow additional
// [ModuleInstance] functionality.
type ModuleInstance interface {
	// RequestMiddleware returns [proxy.MiddlewareFunc] that is run on each incoming request
	// before making the routing decision. It is also a way how to mangle with all incoming traffic.
	RequestMiddleware() (proxy.MiddlewareFunc, error)

	// IngressMiddleware returns [proxy.MiddlewareFunc] for given [*netv1.Ingress] that is run on
	// each request which would be proxied to the Ingress endpoints.
	IngressMiddleware(reconciler IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error)

	// implementsModuleInstance is a method to verify that all implementations of [ModuleInstance]
	// embed [Module] to make extending the [ModuleInstance] easier.
	implementsModuleInstance()
}

// Module is an empty module and is required to be included in any other module.
type Module struct{}

// RequestMiddleware provides default implementation of [ModuleInstance].
func (e Module) RequestMiddleware() (proxy.MiddlewareFunc, error) {
	return nil, nil
}

// IngressMiddleware provides default implementation of [ModuleInstance].
func (e Module) IngressMiddleware(reconciler IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	return nil, nil
}

// implementsModuleInstance is a method to satisfy [ModuleInstance].
func (e Module) implementsModuleInstance() {
}

// ensure [Module] satisfies [ModuleInstance]
var _ ModuleInstance = Module{}

// IngressReconciler is an interface that provides access to some further information
// in the configuration phase of the module. It proxies the requests directly to the
// K8s API, but assures that if some object is used during config, the Ingress will be
// reconciled on change of the object.
type IngressReconciler interface {
	GetSecret(secret types.NamespacedName) (*v1.Secret, error)
}
