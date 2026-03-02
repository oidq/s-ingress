package config

import "k8s.io/apimachinery/pkg/types"

// ControllerEnvConf is the initial controller configuration received from environment on startup.
type ControllerEnvConf struct {
	Namespace      string
	ControllerName string

	ControllerConfigMap types.NamespacedName
}
