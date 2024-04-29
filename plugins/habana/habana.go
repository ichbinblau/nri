/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"
	"github.com/containerd/nri/plugins/habana/config"
	"github.com/containerd/nri/plugins/habana/discover"
)

const (
	hookDefaultFilePath = "/usr/bin/habana-container-hook"
	defaultL3Config     = "/etc/habanalabs/gaudinet.json"

	EnvHLVisibleDevices = "HABANA_VISIBLE_DEVICES"
	EnvHLVisibleModules = "HABANA_VISIBLE_MODULES"
	EnvHLRuntimeError   = "HABANA_RUNTIME_ERROR"
)

var (
	log       *logrus.Logger
	osReadDir = os.ReadDir
	// execRunc     = execRuncFunc
	execLookPath = exec.LookPath
	osStat       = os.Stat
	osExecutable = os.Executable
)

// our injector plugin
type plugin struct {
	stub stub.Stub
	cfg  *config.Config
}

func (p *plugin) Synchronize(_ context.Context, pods []*api.PodSandbox, containers []*api.Container) ([]*api.ContainerUpdate, error) {
	for _, pod := range pods {
		log.Info("Synchronize ", "pods:", pod.Name)
	}
	return nil, nil
}

func NeedFakeDevice() bool {
	_, need := os.LookupEnv("FAKE_DEVICE")
	return need
}

// CreateContainer handles container creation requests.
func (p *plugin) CreateContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	log.Info("CreateContainer ", " pod:", pod.Name, ", containers:", container.Name)
	adjust := &api.ContainerAdjustment{}

	// If user didn't ask specifically for always trying to mount the devices
	// to each container, skip. This keeps the environment and runtime flow cleaner,
	// and skips containers that do not asked for devices.
	if !p.cfg.Runtime.AlwaysMount && !IsHabanaContainer(container) {
		// skip if not always mounting and not a Habana container
		return nil, nil, nil
	}

	// If legacy mode, add habana-hook as a prestart hook, and return to
	// execute runc. The hook and libhabana takes cares of the devices mounts.
	if p.cfg.Runtime.Mode == config.ModeLegacy {
		log.Info("In legacy mode")
		err := addPrestartHook(adjust, p.cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("adding habana prestart hook: %s", err)
		}
		log.Info(adjust)
		return adjust, nil, nil
	}

	// Always add this hook to expose network interfaces information
	// inside the container
	err := addCreateRuntimeHook(adjust, p.cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("adding createRuntime hook: %w", err)
	}

	log.Info("Adjusted hooks: ", adjust)

	// We get the available devices based on the user request. If requested device is not
	// available, we'll return here and log the info. If the options is 'all' or not set,
	// we get all the devices.
	var requestedDevices []string
	fake := NeedFakeDevice()
	if !fake {
		requestedDevices = discover.DevicesIDs(filterDevicesByENV(container, discover.AcceleratorDevices()))
	} else {
		requestedDevices = []string{"0", "1", "2", "3", "4", "5", "6", "7"}
	}
	if len(requestedDevices) == 0 {
		log.Info("No habanalabs accelerators found")
		return nil, nil, nil
	}
	log.Debug("Requested devices: ", requestedDevices)

	if p.cfg.MountAccelerators {
		err = addAcceleratorDevices(adjust, requestedDevices, fake)
		if err != nil {
			addErrorEnvVar(container, adjust, err.Error())
			return nil, nil, fmt.Errorf("adding accelerator devices: %w", err)
		}
	}

	if p.cfg.MountUverbs {
		err = addUverbsDevices(adjust, requestedDevices, fake)
		if err != nil {
			addErrorEnvVar(container, adjust, err.Error())
			return nil, nil, fmt.Errorf("adding uverb devices: %w", err)
		}
	}

	log.Info(adjust)

	return adjust, nil, nil
}

func IsHabanaContainer(c *api.Container) bool {
	for _, ev := range c.Env {
		if strings.HasPrefix(ev, EnvHLVisibleDevices) {
			return true
		}
	}
	return false
}

func addErrorEnvVar(c *api.Container, adj *api.ContainerAdjustment, msg string) {
	for _, env := range c.Env {
		if strings.HasPrefix(env, EnvHLRuntimeError) {
			return
		}
	}
	addEnvVar(adj, EnvHLRuntimeError, msg)
}

func main() {
	var (
		pluginName string
		pluginIdx  string
		opts       []stub.Option
		err        error
	)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}

	log = logrus.StandardLogger()
	log.SetFormatter(&logrus.TextFormatter{
		PadLevelText: true,
	})
	log.SetLevel(cfg.Runtime.LogLevel)

	flag.StringVar(&pluginName, "name", "", "plugin name to register to NRI")
	flag.StringVar(&pluginIdx, "idx", "", "plugin index to register to NRI")
	flag.Parse()

	if pluginName != "" {
		opts = append(opts, stub.WithPluginName(pluginName))
	}
	if pluginIdx != "" {
		opts = append(opts, stub.WithPluginIdx(pluginIdx))
	}

	p := &plugin{cfg: cfg}
	if p.stub, err = stub.New(p, opts...); err != nil {
		log.Fatalf("failed to create plugin stub: %v", err)
	}

	err = p.stub.Run(context.Background())
	if err != nil {
		log.Errorf("plugin exited with error %v", err)
		os.Exit(1)
	}
}
