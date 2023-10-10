// Copyright 2022 Google LLC
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

package status

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"kpt.dev/resourcegroup/controllers/resourcemap"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/yaml"

	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
)

const (
	owningInventoryKey      = "config.k8s.io/owning-inventory"
	SourceHashAnnotationKey = "configmanagement.gke.io/token"
)

// ComputeStatus computes the status and conditions that should be
// saved in the memory.
func ComputeStatus(obj *unstructured.Unstructured) *resourcemap.CachedStatus {
	resStatus := &resourcemap.CachedStatus{}

	// get the resource status using the kstatus library
	result, err := kstatus.Compute(obj)
	if err != nil || result == nil {
		resStatus.Status = v1alpha1.Unknown
	}
	if err != nil {
		klog.Errorf("kstatus.Compute for %v failed: %v", obj, err)
	}
	if err != nil || result == nil {
		resStatus.Status = v1alpha1.Unknown
		return resStatus
	}

	resStatus.Status = v1alpha1.Status(result.Status)
	if resStatus.Status == v1alpha1.Failed {
		resStatus.Conditions = ConvertKstatusConditions(result.Conditions)
	} else if IsCNRMResource(obj.GroupVersionKind().Group) && resStatus.Status != v1alpha1.Current {
		// Special handling for KCC resources.
		// It should be removed after KCC resources implement the stalled conditions.
		conditions, cErr := ReadKCCResourceConditions(obj)
		if cErr != nil {
			klog.Errorf(cErr.Error())
			// fallback to use the kstatus conditions for this resource.
			resStatus.Conditions = ConvertKstatusConditions(result.Conditions)
		} else {
			resStatus.Conditions = conditions
		}
	}

	hash := GetSourceHash(obj.GetAnnotations())
	if hash != "" {
		resStatus.SourceHash = hash
	}
	// get the inventory ID.
	inv := getOwningInventory(obj.GetAnnotations())
	resStatus.InventoryID = inv
	return resStatus
}

// ConvertKstatusConditions converts the status from kstatus library to the conditions
// defined in ResourceGroup apis.
func ConvertKstatusConditions(kstatusConds []kstatus.Condition) []v1alpha1.Condition {
	var result []v1alpha1.Condition
	for _, cond := range kstatusConds {
		result = append(result, convertKstatusCondition(cond))
	}
	return result
}

func convertKstatusCondition(kstatusCond kstatus.Condition) v1alpha1.Condition {
	return v1alpha1.Condition{
		Type:    v1alpha1.ConditionType(kstatusCond.Type),
		Status:  v1alpha1.ConditionStatus(kstatusCond.Status),
		Reason:  kstatusCond.Reason,
		Message: kstatusCond.Message,
		// When kstatus adds the support for accepting an existing list of conditions and
		// compute `LastTransitionTime`, we can set LastTransitionTime to:
		// LastTransitionTime: kstatusCond.LastTransionTime,
		// Leaving LastTransitionTime unset or setting it as `metav1.Time{}` or `metav1.Time{Time: time.Time{}}` will cause serialization error:
		//     status.resourceStatuses.conditions.lastTransitionTime: Invalid value: \"null\":
		//     status.resourceStatuses.conditions.lastTransitionTime in body must be of type string: \"null\""
		LastTransitionTime: metav1.Now(),
	}
}

// IsCNRMResource checks if a group is for a CNRM resource.
func IsCNRMResource(group string) bool {
	return strings.HasSuffix(group, "cnrm.cloud.google.com")
}

// ReadKCCResourceConditions reads the status.conditions from a KCC object.
func ReadKCCResourceConditions(obj *unstructured.Unstructured) ([]v1alpha1.Condition, error) {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return nil, fmt.Errorf("failed to find .stauts.conditions for %s/%s: %v", obj.GetNamespace(), obj.GetName(), err)
	}
	if !found {
		return nil, fmt.Errorf("failed to find .stauts.conditions for %s/%s", obj.GetNamespace(), obj.GetName())
	}
	data, err := yaml.Marshal(conditions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal conditions for %s/%s", obj.GetNamespace(), obj.GetName())
	}
	results := make([]v1alpha1.Condition, len(conditions))
	err = yaml.Unmarshal(data, &results)
	return results, err
}

// GetSourceHash returns the source hash that is defined in the
// source hash annotation.
func GetSourceHash(annotations map[string]string) string {
	if len(annotations) == 0 {
		return ""
	}
	sourceHash := annotations[SourceHashAnnotationKey]
	if len(sourceHash) > 7 {
		return sourceHash[0:7]
	}
	return sourceHash
}

func getOwningInventory(annotations map[string]string) string {
	if len(annotations) == 0 {
		return ""
	}
	return annotations[owningInventoryKey]
}
