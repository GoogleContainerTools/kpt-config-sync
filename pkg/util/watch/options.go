// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package watch

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SingleObjectListOptions builds a set of List options that filter down to one
// specific object.
func SingleObjectListOptions(obj client.Object) *client.ListOptions {
	opts := client.ListOptions{}
	// Use object name and namespace as filters
	key := client.ObjectKeyFromObject(obj)
	if len(key.Namespace) > 0 {
		opts.Namespace = key.Namespace
	}
	if len(key.Name) > 0 {
		opts.FieldSelector = fields.OneTermEqualSelector(metav1.ObjectNameField, key.Name)
	}
	// Use object labels as filters
	labelMap := obj.GetLabels()
	if len(labelMap) > 0 {
		opts.LabelSelector = client.MatchingLabelsSelector{
			Selector: labels.SelectorFromSet(labelMap),
		}
	}
	return &opts
}

// MergeListOptions merges two sets of ListOptions.
// - For Namespace, Limit, and Continue, at most one may be specified, unless they're the same
// - For FieldSelector and LabelSelector, the result requires both filters to match (AND)
// - For Raw, at most one may be specified
func MergeListOptions(a, b *client.ListOptions) (*client.ListOptions, error) {
	switch {
	case a == nil && b == nil:
		return nil, nil
	case a == nil:
		return b, nil
	case b == nil:
		return a, nil
	}

	c := client.ListOptions{}

	switch {
	case a.LabelSelector != nil && b.LabelSelector != nil:
		bReqs, selectable := b.LabelSelector.Requirements()
		if selectable {
			c.LabelSelector = a.LabelSelector.Add(bReqs...)
		} else {
			// not selectable means Matches() always returns false
			c.LabelSelector = labels.Nothing()
		}
	case a.LabelSelector != nil:
		c.LabelSelector = a.LabelSelector
	case b.LabelSelector != nil:
		c.LabelSelector = b.LabelSelector
	}

	switch {
	case a.FieldSelector != nil && b.FieldSelector != nil:
		if a.FieldSelector == fields.Nothing() || b.FieldSelector == fields.Nothing() {
			// Nothing means Matches() always returns false
			c.FieldSelector = fields.Nothing()
		} else {
			c.FieldSelector = fields.AndSelectors(a.FieldSelector, b.FieldSelector)
		}
	case a.FieldSelector != nil:
		c.FieldSelector = a.FieldSelector
	case b.FieldSelector != nil:
		c.FieldSelector = b.FieldSelector
	}

	switch {
	case a.Namespace != "" && b.Namespace != "":
		if a.Namespace != b.Namespace {
			return nil, fmt.Errorf("cannot merge two different namespaces: %s & %s",
				a.Namespace, b.Namespace)
		}
		c.Namespace = a.Namespace
	case a.Namespace != "":
		c.Namespace = a.Namespace
	case b.Namespace != "":
		c.Namespace = b.Namespace
	}

	switch {
	case a.Limit > 0 && b.Limit > 0:
		if a.Limit != b.Limit {
			return nil, fmt.Errorf("cannot merge two different limits: %d & %d",
				a.Limit, b.Limit)
		}
		c.Limit = a.Limit
	case a.Limit > 0:
		c.Limit = a.Limit
	case b.Limit > 0:
		c.Limit = b.Limit
	}

	switch {
	case a.Continue != "" && b.Continue != "":
		if a.Continue != b.Continue {
			return nil, fmt.Errorf("cannot merge two different continue tokens: %s & %s",
				a.Continue, b.Continue)
		}
		c.Continue = a.Continue
	case a.Continue != "":
		c.Continue = a.Continue
	case b.Continue != "":
		c.Continue = b.Continue
	}

	switch {
	case a.Raw != nil && b.Raw != nil:
		// TODO: merge raw ListOptions
		// It should be possible to merge raw ListOptions, but we don't need it
		// yet, because we're only using MergeListOptions in ClientListerWatcher
		// with UntilDeletedWithSync, which merges client.ListOptions from
		// the ClientListerWatcher with metav1.ListOptions from the Reflector
		// used by the Informer built by UntilWithoutRetry.
		return nil, fmt.Errorf("not yet implemented: merging two different raw ListOptions: %+v & %+v",
			a.Raw, b.Raw)
	case a.Raw != nil:
		c.Raw = a.Raw
	case b.Raw != nil:
		c.Raw = b.Raw
	}

	return &c, nil
}

// ConvertListOptions converts from `metav1.ListOptions` to `client.ListOptions`.
//
// For the reverse, see `client.ListOptions.AsListOptions()`.
func ConvertListOptions(mOpts *metav1.ListOptions) (*client.ListOptions, error) {
	if mOpts == nil {
		return nil, nil
	}
	cOpts := client.ListOptions{}

	if mOpts.LabelSelector != "" {
		ls, err := labels.Parse(mOpts.LabelSelector)
		if err != nil {
			return &cOpts, fmt.Errorf("failed to parse LabelSelector: %w", err)
		}
		cOpts.LabelSelector = ls
	}
	if mOpts.FieldSelector != "" {
		fs, err := fields.ParseSelector(mOpts.FieldSelector)
		if err != nil {
			return &cOpts, fmt.Errorf("failed to parse FieldSelector: %w", err)
		}
		cOpts.FieldSelector = fs
	}
	if mOpts.Limit > 0 {
		cOpts.Limit = mOpts.Limit
	}
	if mOpts.Continue != "" {
		cOpts.Continue = mOpts.Continue
	}

	// Pass raw metav1.ListOptions, in case some options are specified that
	// aren't fields of client.ListOptions.
	// The fields on client.ListOptions will take precedence over the raw
	// metav1.ListOptions fields.
	cOpts.Raw = mOpts

	return &cOpts, nil
}

// UnrollListOptions converts from ListOptions to a slice of ListOption.
//
// For the reverse, see `client.ListOptions.ApplyOptions([]client.ListOption)`.
func UnrollListOptions(in *client.ListOptions) []client.ListOption {
	if in == nil {
		return nil
	}
	var out []client.ListOption
	if in.LabelSelector != nil {
		out = append(out, client.MatchingLabelsSelector{Selector: in.LabelSelector})
	}
	if in.FieldSelector != nil {
		out = append(out, client.MatchingFieldsSelector{Selector: in.FieldSelector})
	}
	if in.Namespace != "" {
		out = append(out, client.InNamespace(in.Namespace))
	}
	if in.Limit > 0 {
		out = append(out, client.Limit(in.Limit))
	}
	if in.Continue != "" {
		out = append(out, client.Continue(in.Continue))
	}
	if in.Raw != nil {
		out = append(out, &RawListOptions{Raw: in.Raw})
	}
	return out
}

// RawListOptions wraps a `metav1.ListOptions` to allow passing as
// `client.ListOption` to controller-runtime clients.
type RawListOptions struct {
	Raw *metav1.ListOptions
}

// ApplyToList sets the Raw metav1.ListOptions on the specified
// client.ListOptions.
func (rlo *RawListOptions) ApplyToList(lo *client.ListOptions) {
	if rlo.Raw != nil {
		lo.Raw = rlo.Raw
	}
}
