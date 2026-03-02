package controller

import (
	"k8s.io/apimachinery/pkg/types"
)

func (ic *IngressController) IsSecretRelevant(name types.NamespacedName) bool {
	return false
}

func (ic *IngressController) SetSecret(name types.NamespacedName) {
	//TODO implement me
	panic("implement me")
}

func (ic *IngressController) RemoveSecret(name types.NamespacedName) {
	//TODO implement me
	panic("implement me")
}
