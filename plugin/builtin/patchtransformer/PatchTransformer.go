// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

//go:generate go run sigs.k8s.io/kustomize/v3/cmd/pluginator
package main

import (
	"fmt"
	"github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	"sigs.k8s.io/kustomize/v3/pkg/ifc"
	"sigs.k8s.io/kustomize/v3/pkg/resmap"
	"sigs.k8s.io/kustomize/v3/pkg/resource"
	"sigs.k8s.io/kustomize/v3/pkg/types"
	"sigs.k8s.io/yaml"
)

type plugin struct {
	ldr          ifc.Loader
	rf           *resmap.Factory
	loadedPatch  *resource.Resource
	decodedPatch jsonpatch.Patch
	Path         string          `json:"path,omitempty" yaml:"path,omitempty"`
	Patch        string          `json:"patch,omitempty" yaml:"patch,omitempty"`
	Target       *types.Selector `json:"target,omitempty", yaml:"target,omitempty"`
}

//noinspection GoUnusedGlobalVariable
var KustomizePlugin plugin

func (p *plugin) Config(
	ldr ifc.Loader, rf *resmap.Factory, c []byte) (err error) {
	p.ldr = ldr
	p.rf = rf
	err = yaml.Unmarshal(c, p)
	if err != nil {
		return err
	}
	if p.Patch == "" && p.Path == "" {
		err = fmt.Errorf(
			"must specify one of patch and path in\n%s", string(c))
		return
	}
	if p.Patch != "" && p.Path != "" {
		err = fmt.Errorf(
			"patch and path can't be set at the same time\n%s", string(c))
		return
	}
	var in []byte
	if p.Path != "" {
		in, err = ldr.Load(p.Path)
		if err != nil {
			return
		}
	}
	if p.Patch != "" {
		in = []byte(p.Patch)
	}

	patchSM, errSM := p.rf.RF().FromBytes(in)
	patchJson, errJson := jsonPatchFromBytes(in)
	if errSM != nil && errJson != nil {
		err = fmt.Errorf(
			"unable to get either a Strategic Merge Patch or JSON patch 6902 from %s", p.Patch)
		return
	}
	if errSM == nil && errJson != nil {
		p.loadedPatch = patchSM
	}
	if errJson == nil && errSM != nil {
		p.decodedPatch = patchJson
	}
	if patchSM != nil && patchJson != nil {
		err = fmt.Errorf(
			"a patch can't be both a Strategic Merge Patch and JSON patch 6902 %s", p.Patch)
	}

	return nil
}

func (p *plugin) Transform(m resmap.ResMap) error {
	if p.loadedPatch != nil && p.Target == nil {
		target, err := m.GetById(p.loadedPatch.OrgId())
		if err != nil {
			return err
		}
		err = target.Patch(p.loadedPatch.Kunstructured)
		if err != nil {
			return err
		}
	}

	if p.Target == nil {
		return fmt.Errorf("must specify a target for patch %s", p.Patch)
	}

	resources, err := m.Select(*p.Target)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if p.decodedPatch != nil {
			rawObj, err := resource.MarshalJSON()
			if err != nil {
				return err
			}
			modifiedObj, err := p.decodedPatch.Apply(rawObj)
			if err != nil {
				return errors.Wrapf(
					err, "failed to apply json patch '%s'", p.Patch)
			}
			err = resource.UnmarshalJSON(modifiedObj)
			if err != nil {
				return err
			}
		}
		if p.loadedPatch != nil {
			p.loadedPatch.SetName(resource.GetName())
			p.loadedPatch.SetNamespace(resource.GetNamespace())
			p.loadedPatch.SetGvk(resource.GetGvk())
			err = resource.Patch(p.loadedPatch.Kunstructured)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// jsonPatchFromBytes loads a Json 6902 patch from
// a bytes input
func jsonPatchFromBytes(
	in []byte) (jsonpatch.Patch, error) {
	ops := string(in)
	if ops == "" {
		return nil, fmt.Errorf("empty json patch operations")
	}

	if ops[0] != '[' {
		jsonOps, err := yaml.YAMLToJSON(in)
		if err != nil {
			return nil, err
		}
		ops = string(jsonOps)
	}
	return jsonpatch.DecodePatch([]byte(ops))
}
