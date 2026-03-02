package controller

import (
	"context"
	"fmt"

	"codeberg.org/oidq/s-ingress/modules"
	"codeberg.org/oidq/s-ingress/pkg/config"
	v1 "k8s.io/api/core/v1"
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

	for _, module := range modules.Modules {
		mod, err := module(conf)
		if err != nil {
			return nil, fmt.Errorf("error creating module %T: %w", mod, err)
		}
		conf.Modules = append(conf.Modules, mod)
	}

	return conf, nil
}
