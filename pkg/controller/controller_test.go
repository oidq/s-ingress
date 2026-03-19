package controller

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

type testContext struct {
	ctx        context.Context
	testEnv    *envtest.Environment
	k8sClient  client.Client
	mgr        ctrl.Manager
	controller *IngressController
	log        *slog.Logger
}

func setupTestContext(t *testing.T, nsName string, controllerName string, configYaml string) *testContext {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set")
	}

	ctx := t.Context()
	testEnv := &envtest.Environment{}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start testenv: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("failed to stop testenv: %v", err)
		}
	})

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("failed to create k8s client: %v", err)
	}

	// Setup resources BEFORE starting manager
	createResource(t, ctx, k8sClient, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})

	iClass := &netv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: "s-ingress"},
		Spec:       netv1.IngressClassSpec{Controller: "oidq.com/s-ingress"},
	}
	createResource(t, ctx, k8sClient, iClass)

	if configYaml == "" {
		configYaml = "proxy:\n  maxBodySize: 1048576\n"
	}

	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "controller-config", Namespace: nsName},
		Data:       map[string]string{"config.yaml": configYaml},
	}
	createResource(t, ctx, k8sClient, cm)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-service", Namespace: nsName},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{Name: "http", Port: 80}},
		},
	}
	createResource(t, ctx, k8sClient, svc)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	envConf := config.ControllerEnvConf{
		Namespace:      nsName,
		ControllerName: "oidq.com/s-ingress",
		ControllerConfigMap: types.NamespacedName{
			Name:      "controller-config",
			Namespace: nsName,
		},
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctrl.SetLogger(logr.FromSlogHandler(log.Handler()))

	controller := NewProxyController(log, k8sClient, envConf)
	if err := controller.SetupWithManager(mgr, controllerName); err != nil {
		t.Fatalf("failed to setup controller with manager: %v", err)
	}
	controller.Run(ctx)

	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Error("manager failed", "err", err)
		}
	}()

	return &testContext{
		ctx:        ctx,
		testEnv:    testEnv,
		k8sClient:  k8sClient,
		mgr:        mgr,
		controller: controller,
		log:        log,
	}
}

func createResource(t *testing.T, ctx context.Context, k8sClient client.Client, obj client.Object) {
	t.Helper()
	if err := k8sClient.Create(ctx, obj); err != nil {
		t.Fatalf("failed to create %s: %v", obj.GetObjectKind().GroupVersionKind().Kind, err)
	}
}

func TestIngressControllerManager(t *testing.T) {
	tc := setupTestContext(t, "test-ns", "s-ingress-mgr-test", "")

	ingress := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingress", Namespace: "test-ns"},
		Spec: netv1.IngressSpec{
			IngressClassName: new("s-ingress"),
			Rules: []netv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "test-service",
											Port: netv1.ServiceBackendPort{Number: 80},
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
	createResource(t, tc.ctx, tc.k8sClient, ingress)

	// Wait for reconciliation
	err := wait.PollUntilContextTimeout(tc.ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		proxyConfig, err := tc.controller.getProxyConfig()
		if err != nil {
			return false, nil
		}
		_, ok := proxyConfig.Hosts["example.com"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("failed waiting for reconciliation: %v", err)
	}
}

func TestIngressIgnoreOtherClass(t *testing.T) {
	tc := setupTestContext(t, "test-ns-ignore", "s-ingress-ignore-test", "")

	otherIClass := &netv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: "other-ingress"},
		Spec:       netv1.IngressClassSpec{Controller: "other.com/ingress-controller"},
	}
	createResource(t, tc.ctx, tc.k8sClient, otherIClass)

	// Create ingress with OTHER ingress class
	ingress := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "other-ingress", Namespace: "test-ns-ignore"},
		Spec: netv1.IngressSpec{
			IngressClassName: new("other-ingress"),
			Rules: []netv1.IngressRule{
				{
					Host: "other.example.com",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "test-service",
											Port: netv1.ServiceBackendPort{Number: 80},
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
	createResource(t, tc.ctx, tc.k8sClient, ingress)

	// Create ingress with OUR ingress class to verify controller is working
	ourIngress := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "our-ingress", Namespace: "test-ns-ignore"},
		Spec: netv1.IngressSpec{
			IngressClassName: new("s-ingress"),
			Rules: []netv1.IngressRule{
				{
					Host: "our.example.com",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "test-service",
											Port: netv1.ServiceBackendPort{Number: 80},
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
	createResource(t, tc.ctx, tc.k8sClient, ourIngress)

	// Wait for our ingress to be reconciled
	err := wait.PollUntilContextTimeout(tc.ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		proxyConfig, err := tc.controller.getProxyConfig()
		if err != nil {
			return false, nil
		}
		_, ok := proxyConfig.Hosts["our.example.com"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("failed waiting for our ingress reconciliation: %v", err)
	}

	// Verify other ingress is NOT in the config
	proxyConfig, err := tc.controller.getProxyConfig()
	if err != nil {
		t.Fatalf("failed to get proxy config: %v", err)
	}
	if _, ok := proxyConfig.Hosts["other.example.com"]; ok {
		t.Fatalf("ingress from other class was reconciled")
	}
}

func TestIngressUpdateReconciliation(t *testing.T) {
	tc := setupTestContext(t, "test-ns-update", "s-ingress-update-test", "")

	ingress := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingress", Namespace: "test-ns-update"},
		Spec: netv1.IngressSpec{
			IngressClassName: new("s-ingress"),
			Rules: []netv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "test-service",
											Port: netv1.ServiceBackendPort{Number: 80},
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
	createResource(t, tc.ctx, tc.k8sClient, ingress)

	// Wait for initial reconciliation
	err := wait.PollUntilContextTimeout(tc.ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		proxyConfig, err := tc.controller.getProxyConfig()
		if err != nil {
			return false, nil
		}
		_, ok := proxyConfig.Hosts["example.com"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("failed waiting for initial reconciliation: %v", err)
	}

	// Update the Ingress: change host
	ingress.Spec.Rules[0].Host = "updated-example.com"
	if err := tc.k8sClient.Update(tc.ctx, ingress); err != nil {
		t.Fatalf("failed to update ingress: %v", err)
	}

	// Wait for reconciliation of the update
	err = wait.PollUntilContextTimeout(tc.ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		proxyConfig, err := tc.controller.getProxyConfig()
		if err != nil {
			return false, nil
		}
		_, ok := proxyConfig.Hosts["updated-example.com"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("failed waiting for reconciliation of update: %v", err)
	}

	// Verify old host is gone
	proxyConfig, err := tc.controller.getProxyConfig()
	if err != nil {
		t.Fatalf("failed to get proxy config: %v", err)
	}
	if _, ok := proxyConfig.Hosts["example.com"]; ok {
		t.Fatalf("old host still present in proxy config after update")
	}
}

func TestDisableStatusUpdate(t *testing.T) {
	nsName := "test-ns-disable-status"
	configYaml := `
upstreamServiceName: test-service
disableStatusUpdate: true
proxy:
  maxBodySize: 1048576
`
	tc := setupTestContext(t, nsName, "s-ingress-disable-status-test", configYaml)

	ingress := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingress", Namespace: nsName},
		Spec: netv1.IngressSpec{
			IngressClassName: new("s-ingress"),
			Rules: []netv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "test-service",
											Port: netv1.ServiceBackendPort{Number: 80},
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
	createResource(t, tc.ctx, tc.k8sClient, ingress)

	// Wait for reconciliation
	err := wait.PollUntilContextTimeout(tc.ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		proxyConfig, err := tc.controller.getProxyConfig()
		if err != nil {
			return false, nil
		}
		_, ok := proxyConfig.Hosts["example.com"]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("failed waiting for reconciliation: %v", err)
	}

	// Verify status is NOT updated
	updatedIngress := &netv1.Ingress{}
	err = tc.k8sClient.Get(tc.ctx, types.NamespacedName{Name: "test-ingress", Namespace: nsName}, updatedIngress)
	if err != nil {
		t.Fatalf("failed to get ingress: %v", err)
	}

	if len(updatedIngress.Status.LoadBalancer.Ingress) > 0 {
		t.Fatalf("ingress status was updated but should have been disabled")
	}
}

func TestEnableStatusUpdate(t *testing.T) {
	nsName := "test-ns-enable-status"
	configYaml := `
upstreamServiceName: upstream-service
disableStatusUpdate: false
proxy:
  maxBodySize: 1048576
`
	tc := setupTestContext(t, nsName, "s-ingress-enable-status-test", configYaml)

	// Create upstream service with an IP in its status
	upstreamSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "upstream-service", Namespace: nsName},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{Name: "http", Port: 80}},
		},
	}
	createResource(t, tc.ctx, tc.k8sClient, upstreamSvc)

	upstreamSvc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{
		{IP: "1.2.3.4"},
	}
	if err := tc.k8sClient.Status().Update(tc.ctx, upstreamSvc); err != nil {
		t.Fatalf("failed to update upstream service status: %v", err)
	}

	ingress := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ingress", Namespace: nsName},
		Spec: netv1.IngressSpec{
			IngressClassName: new("s-ingress"),
			Rules: []netv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: new(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "test-service",
											Port: netv1.ServiceBackendPort{Number: 80},
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
	createResource(t, tc.ctx, tc.k8sClient, ingress)

	// Wait for status update
	err := wait.PollUntilContextTimeout(tc.ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		updatedIngress := &netv1.Ingress{}
		err := tc.k8sClient.Get(ctx, types.NamespacedName{Name: "test-ingress", Namespace: nsName}, updatedIngress)
		if err != nil {
			return false, nil
		}
		return len(updatedIngress.Status.LoadBalancer.Ingress) > 0, nil
	})
	if err != nil {
		t.Fatalf("failed waiting for ingress status update: %v", err)
	}

	// Verify status IP
	updatedIngress := &netv1.Ingress{}
	err = tc.k8sClient.Get(tc.ctx, types.NamespacedName{Name: "test-ingress", Namespace: nsName}, updatedIngress)
	if err != nil {
		t.Fatalf("failed to get ingress: %v", err)
	}
	if updatedIngress.Status.LoadBalancer.Ingress[0].IP != "1.2.3.4" {
		t.Fatalf("unexpected ingress status IP: got %s, want 1.2.3.4", updatedIngress.Status.LoadBalancer.Ingress[0].IP)
	}
}
