CASE=basic

CLUSTER=${CASE}
CLUSTER_DIR=${CASE}-cluster
KUBECONFIG=$(shell pwd)/kubeconfig-${CASE}.yaml
KUBECTL=kubectl --kubeconfig=${KUBECONFIG}

build-image:
	docker build -t codeberg.org/oidq/s-ingress:local ..

push-image: build-image
	kind load docker-image --name ${CLUSTER} codeberg.org/oidq/s-ingress:local

start:
	kind create cluster --name ${CLUSTER} --config=${CLUSTER_DIR}/cluster.yaml --kubeconfig=${KUBECONFIG}

deploy: push-image
	${KUBECTL} kustomize --enable-helm ${CLUSTER_DIR}/config --helm-command ~/bin/helm/helm | ${KUBECTL} apply -f -
	${KUBECTL} wait --for=condition=Ready -n s-ingress --all pod

run-test:
	KUBECONFIG=${KUBECONFIG} go tool ginkgo -v ./${CLUSTER_DIR}

destroy:
	kind delete cluster --name ${CLUSTER}
	rm ${KUBECONFIG}

clean: destroy

e2e: start deploy run-test