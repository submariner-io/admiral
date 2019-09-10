#!/usr/bin/env bash
set -em

source $(dirname $0)/../lib/debug_functions

### Functions ###

function kind_clusters() {
    trap_commands
    status=$1
    version=$2
    pids=(-1 -1)
    logs=()
    for i in 1 2; do
        if [[ $(kind get clusters | grep cluster${i} | wc -l) -gt 0  ]]; then
            echo "Cluster cluster${i} already exists, skipping cluster creation..."
        else
            logs[$i]=$(mktemp)
            echo Creating cluster${i}, logging to ${logs[$i]}...
            (
            if [[ -n ${version} ]]; then
                kind create cluster --image=kindest/node:v${version} --name=cluster${i} --wait=5m --config=${PRJ_ROOT}/scripts/kind-e2e/config/cluster${i}-config.yaml
            else
                kind create cluster --name=cluster${i} --wait=5m --config=${PRJ_ROOT}/scripts/kind-e2e/config/cluster${i}-config.yaml
            fi
            master_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cluster${i}-control-plane | head -n 1)
            sed -i -- "s/user: kubernetes-admin/user: cluster$i/g" $(kind get kubeconfig-path --name="cluster$i")
            sed -i -- "s/name: kubernetes-admin.*/name: cluster$i/g" $(kind get kubeconfig-path --name="cluster$i")
            sed -i -- "s/current-context: kubernetes-admin.*/current-context: cluster$i/g" $(kind get kubeconfig-path --name="cluster$i")

            if [[ ${status} = keep ]]; then
                cp -r $(kind get kubeconfig-path --name="cluster$i") ${PRJ_ROOT}/output/kind-config/local-dev/kind-config-cluster${i}
            fi

            sed -i -- "s/server: .*/server: https:\/\/$master_ip:6443/g" $(kind get kubeconfig-path --name="cluster$i")
            cp -r $(kind get kubeconfig-path --name="cluster$i") ${PRJ_ROOT}/output/kind-config/dapper/kind-config-cluster${i}
            ) > ${logs[$i]} 2>&1 &
            set pids[$i] = $!
        fi
    done
    if [[ ${#logs[@]} -gt 0 ]]; then
        echo "(Watch the installation processes with \"tail -f ${logs[@]}\".)"
        for i in 1 2; do
            if [[ pids[$i] -gt -1 ]]; then
                wait ${pids[$i]}
                if [[ $? -ne 0 && $? -ne 127 ]]; then
                    echo Cluster $i creation failed:
                    cat $logs[$i]
                fi
                rm -f $logs[$i]
            fi
        done
    fi
}

function install_helm() {
    trap_commands
    helm init --client-only
    helm repo add submariner-latest https://releases.rancher.com/submariner-charts/latest
    pids=(-1 -1)
    logs=()
    for i in 1 2; do
        if kubectl --context=cluster${i} -n kube-system rollout status deploy/tiller-deploy > /dev/null 2>&1; then
            echo "Helm already installed on cluster${i}, skipping helm installation..."
        else
            logs[$i]=$(mktemp)
            echo "Installing helm on cluster${i}, logging to ${logs[$i]}..."
            (
            kubectl --context=cluster${i} -n kube-system create serviceaccount tiller
            kubectl --context=cluster${i} create clusterrolebinding tiller --clusterrole=cluster-admin --serviceaccount=kube-system:tiller
            helm --kube-context cluster${i} init --service-account tiller
            kubectl --context=cluster${i} -n kube-system rollout status deploy/tiller-deploy
            ) > ${logs[$i]} 2>&1 &
            set pids[$i] = $!
        fi
    done
    if [[ ${#logs[@]} -gt 0 ]]; then
        echo "(Watch the installation processes with \"tail -f ${logs[@]}\".)"
        for i in 1 2; do
            if [[ pids[$i] -gt -1 ]]; then
                wait ${pids[$i]}
                if [[ $? -ne 0 && $? -ne 127 ]]; then
                    echo Cluster $i creation failed:
                    cat $logs[$i]
                fi
                rm -f $logs[$i]
            fi
        done
    fi
}

function enable_kubefed() {
    trap_commands
    if kubectl --context=cluster1 rollout status deploy/kubefed-controller-manager -n ${KUBEFED_NS} > /dev/null 2>&1; then
        echo "Kubefed already installed, skipping setup..."
    else
        helm repo add kubefed-charts https://raw.githubusercontent.com/kubernetes-sigs/kubefed/master/charts
        helm --kube-context cluster1 install kubefed-charts/kubefed --version=0.1.0-rc2 --name kubefed --namespace ${KUBEFED_NS} --set controllermanager.replicaCount=1
        for i in 1 2; do
            kubefedctl join cluster${i} --cluster-context cluster${i} --host-cluster-context cluster1 --v=2
        done
        echo "Waiting for kubefed control plain to be ready..."
        kubectl --context=cluster1 wait --for=condition=Ready pods -l control-plane=controller-manager -n ${KUBEFED_NS} --timeout=120s
        kubectl --context=cluster1 wait --for=condition=Ready pods -l kubefed-admission-webhook=true -n ${KUBEFED_NS} --timeout=120s
        kubefedctl enable pods secrets serviceaccount replicaset
    fi
}

function test_with_e2e_tests {
    trap_commands
    cd ../test/e2e

    # Setup the KUBECONFIG env
    export KUBECONFIG=$(echo ${PRJ_ROOT}/output/kind-config/dapper/kind-config-cluster{1..2} | sed 's/ /:/g')

    go test -args -ginkgo.v -ginkgo.randomizeAllSpecs \
        -dp-context cluster1 -dp-context cluster2  \
        -report-dir ${DAPPER_SOURCE}/${DAPPER_OUTPUT}/junit 2>&1 | \
        tee ${DAPPER_SOURCE}/${DAPPER_OUTPUT}/e2e-tests.log
}

function cleanup {
    trap_commands
    for i in 1 2; do
      if [[ $(kind get clusters | grep cluster${i} | wc -l) -gt 0  ]]; then
        kind delete cluster --name=cluster${i};
      fi
    done

    if [[ $(docker ps -qf status=exited | wc -l) -gt 0 ]]; then
        echo Cleaning containers...
        docker ps -qf status=exited | xargs docker rm -f
    fi
    if [[ $(docker images -qf dangling=true | wc -l) -gt 0 ]]; then
        echo Cleaning images...
        docker images -qf dangling=true | xargs docker rmi -f
    fi
    if [[ $(docker volume ls -qf dangling=true | wc -l) -gt 0 ]]; then
        echo Cleaning volumes...
        docker volume ls -qf dangling=true | xargs docker volume rm -f
    fi
}

### Main ###

if [[ $1 = clean ]]; then
    cleanup
    exit 0
fi

if [[ $1 != keep ]]; then
    trap cleanup EXIT
fi

echo Starting with status: $1, k8s_version: $2, kubefed: $3.
PRJ_ROOT=$(git rev-parse --show-toplevel)
mkdir -p ${PRJ_ROOT}/output/kind-config/dapper/ ${PRJ_ROOT}/output/kind-config/local-dev/
KUBEFED_NS=kube-federation-system
export KUBECONFIG=$(echo ${PRJ_ROOT}/output/kind-config/dapper/kind-config-cluster{1..2} | sed 's/ /:/g')

kind_clusters "$@"
install_helm
if [[ $3 = true ]]; then
    enable_kubefed
fi
#test_with_e2e_tests

if [[ $1 = keep ]]; then
    echo "your 2 virtual clusters are deployed and working properly."
    echo "clusters can be accessed with:"
    echo ""
    echo "export KUBECONFIG=\$(echo \$(git rev-parse --show-toplevel)/output/kind-config/local-dev/kind-config-cluster{1..2} | sed 's/ /:/g')"
    echo ""
    echo "$ kubectl config use-context cluster1 # or cluster2.."
    echo ""
    echo "to cleanup, just run: make e2e status=clean"
fi
