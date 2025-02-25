// Copyright 2018 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

// NOTE: you do not need to edit this file when implementing a custom sd.
import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

type customSD struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

// Adapter runs an unknown service discovery implementation and converts its target groups
// to JSON and writes to a file for file_sd.
type Adapter struct {
	ctx     context.Context
	disc    discovery.Discoverer
	groups  map[string]*customSD
	manager *discovery.Manager
	output  string
	name    string
	logger  log.Logger
}

func mapToArray(m map[string]*customSD) []customSD {
	arr := make([]customSD, 0, len(m))
	for _, v := range m {
		arr = append(arr, *v)
	}
	return arr
}

// Parses incoming target groups updates. If the update contains changes to the target groups
// Adapter already knows about, or new target groups, we Marshal to JSON and write to file.
func (a *Adapter) generateTargetGroups(allTargetGroups map[string][]*targetgroup.Group) {
	tempGroups := make(map[string]*customSD)
	for k, sdTargetGroups := range allTargetGroups {
		for i, group := range sdTargetGroups {
			newTargets := make([]string, 0)
			newLabels := make(map[string]string)

			for _, targets := range group.Targets {
				for _, target := range targets {
					newTargets = append(newTargets, string(target))
				}
			}

			for name, value := range group.Labels {
				newLabels[string(name)] = string(value)
			}
			// Make a unique key, including the current index, in case the sd_type (map key) and group.Source is not unique.
			key := fmt.Sprintf("%s:%s:%d", k, group.Source, i)
			tempGroups[key] = &customSD{
				Targets: newTargets,
				Labels:  newLabels,
			}
		}
	}
	if !reflect.DeepEqual(a.groups, tempGroups) {
		a.groups = tempGroups
		err := a.writeOutput()
		if err != nil {
			level.Error(log.With(a.logger, "component", "sd-adapter")).Log("err", err)
		}
	}

}

// Writes JSON formatted targets to output file.
func (a *Adapter) writeOutput() error {
	arr := mapToArray(a.groups)
	b, _ := json.MarshalIndent(arr, "", "    ")

	tmpfile, err := ioutil.TempFile("", "sd-adapter")
	if err != nil {
		return err
	}
	defer tmpfile.Close()

	_, err = tmpfile.Write(b)
	if err != nil {
		return err
	}

	err = os.Chmod(a.output, 0644)
	if err != nil {
		return err
	}

	err = os.Rename(tmpfile.Name(), a.output)
	if err != nil {
		return err
	}

	return nil
}

func (a *Adapter) runCustomSD(ctx context.Context) {
	updates := a.manager.SyncCh()
	for {
		select {
		case <-ctx.Done():
		case allTargetGroups, ok := <-updates:
			// Handle the case that a target provider exits and closes the channel
			// before the context is done.
			if !ok {
				return
			}
			a.generateTargetGroups(allTargetGroups)
		}
	}
}

// Run starts a Discovery Manager and the custom service discovery implementation.
func (a *Adapter) Run() {
	go a.manager.Run()
	a.manager.StartCustomProvider(a.ctx, a.name, a.disc)
	go a.runCustomSD(a.ctx)
}

// NewAdapter creates a new instance of Adapter.
func NewAdapter(ctx context.Context, file string, name string, d discovery.Discoverer, logger log.Logger) *Adapter {
	return &Adapter{
		ctx:     ctx,
		disc:    d,
		groups:  make(map[string]*customSD),
		manager: discovery.NewManager(ctx, logger),
		output:  file,
		name:    name,
		logger:  logger,
	}
}
