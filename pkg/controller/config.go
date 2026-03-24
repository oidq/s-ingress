package controller

import (
	"context"
	"fmt"

	"codeberg.org/oidq/s-ingress/modules"
	"codeberg.org/oidq/s-ingress/pkg/config"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (ic *IngressController) getControllerConfig(ctx context.Context) (*config.ControllerConf, error) {
	confMap := &v1.ConfigMap{}
	err := ic.k8sClient.Get(
		ctx,
		ic.envConfig.ControllerConfigMap,
		confMap,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting config-map: %w", err)
	}

	rawConf, ok := confMap.Data["config.yaml"]
	if !ok {
		return nil, fmt.Errorf("could not find config.yaml in controller config map %s", ic.envConfig.ControllerConfigMap)
	}

	conf, err := config.ParseYamlConfig([]byte(rawConf))
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling config.yaml: %w", err)
	}

	reconciler := &moduleReconciler{
		k8sClient: ic.k8sClient,
		namespace: ic.envConfig.Namespace,
	}

	for _, module := range modules.Modules {
		mod, err := module(ctx, reconciler, conf)
		if err != nil {
			return nil, fmt.Errorf("error creating module %T: %w", mod, err)
		}
		conf.Modules = append(conf.Modules, mod)
	}

	conf.UsedSecrets = reconciler.usedSecrets

	return conf, nil
}

type moduleReconciler struct {
	k8sClient client.Client

	namespace   string
	usedSecrets []types.NamespacedName
}

func (m *moduleReconciler) GetSecret(ctx context.Context, secret types.NamespacedName) (*v1.Secret, error) {
	s := &v1.Secret{}
	err := m.k8sClient.Get(ctx, secret, s)
	if err != nil {
		return nil, fmt.Errorf("error getting secret %s: %v", secret, err)
	}

	m.usedSecrets = append(m.usedSecrets, secret)
	return s, nil
}

func (m *moduleReconciler) GetNamespace() string {
	return m.namespace
}
