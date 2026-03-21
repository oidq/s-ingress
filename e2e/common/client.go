package common

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"

	"github.com/onsi/ginkgo/v2"
	netv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"

	. "github.com/onsi/gomega"
)

func GetClient() *kubernetes.Clientset {
	kubeConf := os.Getenv("KUBECONFIG")
	Expect(kubeConf).ToNot(BeEmpty())

	c, err := getClient(kubeConf)
	Expect(err).ToNot(HaveOccurred())

	return c
}

func getClient(kubeConfFilename string) (*kubernetes.Clientset, error) {
	kubeConfRaw, err := os.ReadFile(kubeConfFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to read KUBECONFIG from %s: %v", kubeConfFilename, err)
	}

	conf, err := clientcmd.RESTConfigFromKubeConfig(kubeConfRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse KUBECONFIG from %s: %v", kubeConfFilename, err)
	}

	return kubernetes.NewForConfig(conf)
}

func NewRequest(method, url string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, url, body)
	Expect(err).ToNot(HaveOccurred(), "failed to create request")
	return req
}

func RoundTripQuic(endpoint netip.AddrPort, req *http.Request) *http.Response {
	q := GetQuicTransport(endpoint)

	resp, err := q.RoundTrip(req)
	Expect(err).ToNot(HaveOccurred(), "failed to send request")

	return resp
}

func ApplyIngress(data string) {
	c := GetClient()

	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(data), nil, nil)
	Expect(err).ToNot(HaveOccurred())

	ing := obj.(*netv1.Ingress)

	_, err = c.NetworkingV1().Ingresses("default").Create(context.Background(), ing, v1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	ginkgo.DeferCleanup(func() {
		err = c.NetworkingV1().Ingresses("default").Delete(context.Background(), ing.Name, v1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})
}

func PatchIngress(name, jsonPatch string) {
	c := GetClient()

	_, err := c.NetworkingV1().Ingresses("default").
		Patch(context.Background(), name, types.MergePatchType, []byte(jsonPatch), v1.PatchOptions{})
	Expect(err).ToNot(HaveOccurred())
}
