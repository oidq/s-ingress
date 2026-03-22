package common

import (
	"context"
	"fmt"
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

var client *kubernetes.Clientset

func SetupClient() {
	client = GetClient()
}

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

func ApplyIngress(data string) {

	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(data), nil, nil)
	Expect(err).ToNot(HaveOccurred())

	ing := obj.(*netv1.Ingress)

	_, err = client.NetworkingV1().Ingresses("default").Create(context.Background(), ing, v1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	ginkgo.DeferCleanup(func() {
		err = client.NetworkingV1().Ingresses("default").Delete(context.Background(), ing.Name, v1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})
}

func PatchIngress(name, jsonPatch string) {
	_, err := client.NetworkingV1().Ingresses("default").
		Patch(context.Background(), name, types.MergePatchType, []byte(jsonPatch), v1.PatchOptions{})
	Expect(err).ToNot(HaveOccurred())
}
