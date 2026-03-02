package test

import (
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetDummyServiceIngress(name string, host string, annotations map[string]string) *netv1.Ingress {
	return &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: netv1.IngressSpec{
			IngressClassName: new(IngressClass),
			Rules: []netv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: DummyServiceName,
											Port: netv1.ServiceBackendPort{
												Name: DummyServicePort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
