# install crictl if it does not exist 
https://github.com/kubernetes-sigs/cri-tools/blob/master/docs/crictl.md

# create dirs
``` shell
mkdir -p /tmp/gaudi
mkdir -p /run/containerd/io.containerd.runtime.v2.task/k8s.io
mkdir -p /sys/fs/cgroup/kubepods.slice/pod8bbf03da_0cdc_4e98_b902_57266a3df437.slice
```

# verify runtime config in `/etc/habana-container-runtime/config.toml`
# check the existance of the network file `/etc/habanalabs/gaudinet.json`

# replace the habana runtime
``` shell
sudo mv /usr/bin/habana-container-runtime /usr/bin/habana-container-runtime.bk
sudo mv /usr/bin/habana-container-cli /usr/bin/habana-container-cli.bk

sudo cp poc/habana-container-runtime /usr/bin/
sudo cp poc/habana-container-cli /usr/bin
sudo chmod +x /usr/bin/habana-container-runtime
sudo chmod +x /usr/bin/habana-container-cli
```

# change containerd
``` yaml
disabled_plugins = []
version = 2

[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    [plugins."io.containerd.grpc.v1.cri".containerd]
      default_runtime_name = "habana"
      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.habana]
          runtime_type = "io.containerd.runc.v2"
          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.habana.options]
            BinaryName = "/usr/bin/habana-container-runtime"
            SystemdCgroup = true
  [plugins."io.containerd.runtime.v1.linux"]
    runtime = "habana-container-runtime"
  [plugins."io.containerd.nri.v1.nri"]
    config_file = "/etc/nri/nri.conf"
    disable = false
    plugin_path = "/opt/nri/plugins"
    socket_path = "/var/run/nri/nri.sock"
```

# create pod
``` shell 
sudo crictl runp pod-config.json
sudo crictl pods

sudo crictl create b27104bd0bdb5 container-config.json pod-config.json
sudo crictl ps a 

sudo crictl stop xxxxx
sudo crictl rm xxxx
```

# check config.json
cd /run/containerd/io.containerd.runtime.v2.task/k8s.io

# reference
https://kubernetes.io/docs/tasks/debug/debug-cluster/crictl/
