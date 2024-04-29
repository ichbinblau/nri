/*
 * Copyright (c) 2022, HabanaLabs Ltd.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/plugins/habana/config"
	"github.com/containerd/nri/plugins/habana/discover"
)

func addPrestartHook(adj *api.ContainerAdjustment, cfg *config.Config) error {
	// path, err := execLookPath("habana-container-runtime-hook")
	path, err := hookBinaryPath(cfg)
	if err != nil {
		path = hookDefaultFilePath
		_, err = os.Stat(path)
		if err != nil {
			return err
		}
	}
	log.Info("Prestart hook path: ", path)

	args := []string{path}
	if adj.Hooks == nil {
		adj.Hooks = &api.Hooks{}
	} else if len(adj.Hooks.Prestart) != 0 {
		for _, hook := range adj.Hooks.Prestart {
			if !strings.Contains(hook.Path, "habana-container-hook") {
				continue
			}
			log.Info("Existing habana prestart hook in OCI spec file")
			return nil
		}
	}

	adj.Hooks.Prestart = append(adj.Hooks.Prestart, &api.Hook{
		Path: path,
		Args: append(args, "prestart"),
	})

	log.Info("Prestart hook added, executing runc")
	return nil
}

func addCreateRuntimeHook(adj *api.ContainerAdjustment, cfg *config.Config) error {
	path, err := hookBinaryPath(cfg)
	if err != nil {
		path = hookDefaultFilePath
		_, err = os.Stat(path)
		if err != nil {
			return err
		}
	}
	log.Info("hook binary path", "path", path)

	args := []string{path}
	if adj.Hooks == nil {
		adj.Hooks = &api.Hooks{}
	} else if len(adj.Hooks.CreateRuntime) != 0 {
		for _, hook := range adj.Hooks.CreateRuntime {
			if !strings.Contains(hook.Path, "habana-container-hook") {
				continue
			}
			log.Info("Existing habana createRuntime hook in OCI spec file")
			return nil
		}
	}

	adj.Hooks.CreateRuntime = append(adj.Hooks.CreateRuntime, &api.Hook{
		Path: path,
		Args: append(args, "createRuntime"),
	})

	log.Info("createRuntime hook added, executing runc")
	return nil
}

func addAcceleratorDevices(adj *api.ContainerAdjustment, requestedDevs []string, fake bool) error {
	log.Debug("Discovering accelerators")

	// TODO: wait for devs and QA approval
	// // Extract module id for HABANA_VISIBLE_MODULES environment variables
	// modulesIDs := make([]string, 0, len(requestedDevs))
	// for _, acc := range requestedDevs {
	// 	id, err := discover.AcceleratorModuleID(acc)
	// 	if err != nil {
	// 		logger.Debug("discoring modules")
	// 		return err
	// 	}
	// 	modulesIDs = append(modulesIDs, id)
	// }
	// addEnvVar(spec, EnvHLVisibleModules, strings.Join(modulesIDs, ","))

	// Prepare devices in OCI format
	var devs []*discover.DevInfo
	for _, u := range requestedDevs {
		for _, d := range []string{"/dev/accel/accel", "/dev/accel/accel_controlD"} {
			p := fmt.Sprintf("%s%s", d, u)
			log.Info("Adding accelerator device path: ", p)
			var i *discover.DevInfo
			var err error
			if !fake {
				i, err = discover.DeviceInfo(p)
			} else {
				i, err = discover.FakeDeviceInfo(d, u)
			}
			if err != nil {
				return err
			}
			devs = append(devs, i)
		}
	}

	addDevicesToSpec(adj, devs)
	// addAllowList(logger, spec, devs)

	return nil
}

func addUverbsDevices(adj *api.ContainerAdjustment, requestedDevsIDs []string, fake bool) error {
	log.Debug("Discovering uverbs")

	var devs []*discover.DevInfo
	uDev := []string{"/dev/infiniband/uverbs9",
		"/dev/infiniband/uverbs10",
		"/dev/infiniband/uverbs11",
		"/dev/infiniband/uverbs12",
		"/dev/infiniband/uverbs13",
		"/dev/infiniband/uverbs14",
		"/dev/infiniband/uverbs15",
		"/dev/infiniband/uverbs16"}
	for i, v := range requestedDevsIDs {
		var uverbDev string
		if fake {
			uverbDev = uDev[i]
		} else {
			hlib := fmt.Sprintf("/sys/class/infiniband/hlib_%s", v)
			log.Debug("Getting uverbs device for hlib", "hlib", hlib)

			// Extract uverb from hlib device
			uverbs, err := osReadDir(fmt.Sprintf("%s/device/infiniband_verbs", hlib))
			if err != nil {
				log.Error(fmt.Sprintf("Reading hlib directory: %v", err))
				continue
			}
			if len(uverbs) == 0 {
				log.Debug("No uverbs devices found for devices", "device", hlib)
				continue
			}
			uverbDev = fmt.Sprintf("/dev/infiniband/%s", uverbs[0].Name())
		}

		// Prepare devices in OCI format
		log.Info("Adding uverb device path: ", uverbDev)
		var i *discover.DevInfo
		var err error
		if !fake {
			i, err = discover.DeviceInfo(uverbDev)
		} else {
			i, err = discover.FakeDeviceInfo(uverbDev, "")
		}
		if err != nil {
			return err
		}
		log.Info("Adding uverb device path: ", uverbDev)
		devs = append(devs, i)
	}

	addDevicesToSpec(adj, devs)
	// addAllowList(logger, spec, devs)

	return nil
}

func filterDevicesByENV(container *api.Container, devices []string) []string {
	var requestedDevs []string
	for _, ev := range container.Env {
		if strings.HasPrefix(ev, "HABANA_VISIBLE_DEVICES") {
			_, values, found := strings.Cut(ev, "=")
			if found {
				if values == "all" {
					return devices
				} else {
					requestedDevs = strings.Split(values, ",")
				}
			}
			break
		}
	}

	// Case when alwaysMatch is true, and user didn't provide the environment variable
	if len(requestedDevs) == 0 {
		return devices
	}

	var filteredDevices []string
	for _, dev := range devices {
		devID := string(dev[len(dev)-1])
		if slices.Contains(requestedDevs, devID) {
			filteredDevices = append(filteredDevices, dev)
		}
	}

	return filteredDevices
}

// addDevicesToSpec adds list of devices nodes to be created for container.
func addDevicesToSpec(adj *api.ContainerAdjustment, devices []*discover.DevInfo) {
	log.Debug("Mounting devices in spec")
	current := make(map[string]struct{})

	for _, dev := range adj.GetLinux().GetDevices() {
		current[dev.Path] = struct{}{}
	}

	// var devicesToAdd []specs.LinuxDevice
	for _, hlDevice := range devices {
		if _, ok := current[hlDevice.Path]; ok {
			continue
		}

		zeroID := uint32(0)
		adj.AddDevice(&api.LinuxDevice{
			Type:     "c",
			Major:    int64(hlDevice.Major),
			Minor:    int64(hlDevice.Minor),
			FileMode: api.FileMode(hlDevice.FileMode),
			Path:     hlDevice.Path,
			Gid:      api.UInt32(zeroID),
			Uid:      api.UInt32(zeroID),
		})
		log.Debug("Added device to spec", "path", hlDevice.Path)
	}
	// spec.Linux.Devices = append(spec.Linux.Devices, devicesToAdd...)
}

// // addAllowList modifies the Linux devices allow list to cgroup rules.
// func addAllowList(logger *slog.Logger, spec *specs.Spec, devices []*discover.DevInfo) {
// 	logger.Debug("Adding devices to allow list")

// 	current := make(map[string]bool)
// 	for _, dev := range spec.Linux.Resources.Devices {
// 		if dev.Major != nil && dev.Minor != nil {
// 			current[fmt.Sprintf("%d-%d", *dev.Major, *dev.Minor)] = true
// 		}
// 	}

// 	var devsToAdd []specs.LinuxDeviceCgroup
// 	for _, hldev := range devices {
// 		k := fmt.Sprintf("%d-%d", hldev.Major, hldev.Minor)
// 		if _, ok := current[k]; !ok {
// 			major := int64(hldev.Major)
// 			minor := int64(hldev.Minor)
// 			devsToAdd = append(devsToAdd, specs.LinuxDeviceCgroup{
// 				Allow:  true,
// 				Type:   "c",
// 				Major:  &major,
// 				Minor:  &minor,
// 				Access: "rwm",
// 			})
// 			logger.Debug("Added device to allow list", "major", hldev.Major, "minor", hldev.Minor)
// 		}
// 	}

// 	// modify spec
// 	spec.Linux.Resources.Devices = append(spec.Linux.Resources.Devices, devsToAdd...)
// }

func addEnvVar(adj *api.ContainerAdjustment, key string, value string) {
	adj.AddEnv(key, strconv.Quote(value))
}

// hookBinaryPath looks for the binary in the following locations by order:
//
// 1. $PATH environment variable
//
// 2. Same directory of the runtime
//
// 3. binaries-dir value from config file
//
// 4. Default location
func hookBinaryPath(cfg *config.Config) (string, error) {
	// Search in PATH
	binPath, err := execLookPath("habana-container-hook")
	if err == nil { // IF NO ERROR
		return binPath, nil
	}

	// Search in the binary habana-container-runtime's dir
	currentExec, err := osExecutable()
	if err == nil { // IF NO ERROR
		currentDir := filepath.Dir(currentExec)
		binPath = path.Join(currentDir, "habana-container-hook")
		if _, err := osStat(binPath); err == nil { // IF NO ERROR
			return binPath, nil
		}
	}

	// Search in the dir provided by binaries-dir
	binPath = path.Join(cfg.BinariesDir, "habana-container-hook")
	if _, err := osStat(binPath); err == nil { // IF NO ERROR
		return binPath, nil
	}

	binPath = hookDefaultFilePath
	_, err = osStat(binPath)
	if err == nil { // IF NO ERROR
		return binPath, nil
	}
	return "", fmt.Errorf("habana-container-hook was not found on the system")
}
