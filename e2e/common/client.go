package common

import (
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
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
