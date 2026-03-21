package test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/controller"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const DummyServiceName = "dummy-service"
const DummyServicePort = "dummy-service"

type TestingContext struct {
	ctx        context.Context
	testEnv    *envtest.Environment
	k8sClient  client.Client
	mgr        ctrl.Manager
	controller *controller.IngressController
	log        *slog.Logger
	proxy      *proxy.Proxy
	t          *testing.T
	ds         *dummyService
}

func setupTestContext(t *testing.T, nsName string, controllerName string, controllerConfig string) *TestingContext {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip()
	}

	ctx := t.Context()
	apiServer := &envtest.APIServer{}
	apiServer.Configure().Set("service-cluster-ip-range", "127.0.0.0/24")
	testEnv := &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer:   apiServer,
			Etcd:        nil,
			KubectlPath: "",
		},
	}
	ds := &dummyService{}
	go ds.start(t)

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
	k8sClient = client.NewNamespacedClient(k8sClient, nsName)

	// Setup resources BEFORE starting manager
	createResource(t, ctx, k8sClient, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})

	iClass := &netv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: IngressClass},
		Spec:       netv1.IngressClassSpec{Controller: "oidq.com/s-ingress"},
	}
	createResource(t, ctx, k8sClient, iClass)

	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "controller-config", Namespace: nsName},
		Data:       map[string]string{"config.yaml": controllerConfig},
	}
	createResource(t, ctx, k8sClient, cm)

	dummy := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: DummyServiceName, Namespace: nsName},
		Spec: v1.ServiceSpec{
			ClusterIP: ds.hostPort().Addr().String(),
			Ports: []v1.ServicePort{
				{
					Name: DummyServiceName,
					Port: int32(ds.hostPort().Port()),
				},
			},
		},
	}
	createService(t, ctx, k8sClient, dummy)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
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

	p := proxy.NewProxy(log)
	go func() {
		err = p.StartDummy(ctx)
		require.ErrorIs(t, err, context.Canceled)
	}()

	controller := controller.NewProxyController(log, k8sClient, envConf)
	if err := controller.SetupWithManager(mgr, controllerName); err != nil {
		t.Fatalf("failed to setup controller with manager: %v", err)
	}
	controller.SetReconfigureChan(p.ConfigChan())
	err = controller.Init(ctx)
	if err != nil {
		t.Fatalf("failed to initialize controller: %v", err)
	}

	go func() {
		controller.Run(ctx)
	}()

	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Error("manager failed", "err", err)
		}
	}()

	return &TestingContext{
		ctx:        ctx,
		testEnv:    testEnv,
		k8sClient:  k8sClient,
		mgr:        mgr,
		controller: controller,
		log:        log,
		proxy:      p,
		t:          t,
		ds:         ds,
	}
}

func createService(t *testing.T, ctx context.Context, k8sClient client.Client, obj *v1.Service) {
	t.Helper()
	if err := k8sClient.Create(ctx, obj); err != nil {
		t.Fatalf("failed to create %s: %v", obj.GetObjectKind().GroupVersionKind().Kind, err)
	}

	// if we created a Service, we need to clean IPAddress objects after it :/
	t.Cleanup(func() {
		ipAddr := &netv1.IPAddress{
			ObjectMeta: metav1.ObjectMeta{
				Name: obj.Spec.ClusterIP,
			},
		}
		err := k8sClient.Delete(context.Background(), ipAddr)
		require.NoError(t, err)

		err = k8sClient.Delete(context.Background(), obj)
		t.Logf("deleted %s", obj.GetName())
		require.NoErrorf(t, err, "cleanup %s failed", obj.GetName())
	})
}

func createResource(t *testing.T, ctx context.Context, k8sClient client.Client, obj client.Object) {
	t.Helper()
	if err := k8sClient.Create(ctx, obj); err != nil {
		t.Fatalf("failed to create %s: %v", obj.GetObjectKind().GroupVersionKind().Kind, err)
	}

	// if we created a Service, we need to clean IPAddress objects after it :/
	t.Cleanup(func() {
		err := k8sClient.Delete(context.Background(), obj)
		t.Logf("deleted %s", obj.GetName())
		require.NoErrorf(t, err, "cleanup %s failed", obj.GetName())
	})
}

const IngressClass = "s-ingress"

func SetupTest(t *testing.T, controllerConfig string, ingresses ...*netv1.Ingress) *TestingContext {
	namespace := Namespace(t)
	tc := setupTestContext(t, namespace, t.Name(), controllerConfig)

	for _, ingress := range ingresses {
		createResource(t, tc.ctx, tc.k8sClient, ingress)
	}

	return tc
}

func (tc *TestingContext) WaitForHostnameConfigured(hostname string) {
	err := wait.PollUntilContextTimeout(tc.t.Context(), 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		return tc.proxy.IsConfiguredForHostname(hostname), nil
	})
	if err != nil {
		tc.t.Fatalf("failed waiting for reconciliation of update: %v", err)
	}
}

func (tc *TestingContext) Serve(req *http.Request) *http.Response {
	w := httptest.NewRecorder()
	tc.proxy.ServeHTTP(w, req)
	return w.Result()
}

func (tc *TestingContext) SetDummyService(handler http.Handler) {
	tc.ds.handler = handler
}

func Namespace(t *testing.T) string {
	ns := strings.ToLower(t.Name())
	ns = strings.ReplaceAll(ns, "_", "-")
	t.Attr("namespace", ns)
	return ns
}
