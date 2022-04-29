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

package configuration

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMerge(t *testing.T) {
	testCases := []struct {
		name        string
		left, right *admissionv1.ValidatingWebhookConfiguration
		want        *admissionv1.ValidatingWebhookConfiguration
	}{
		{
			name:  "empty",
			left:  &admissionv1.ValidatingWebhookConfiguration{},
			right: &admissionv1.ValidatingWebhookConfiguration{},
			want:  &admissionv1.ValidatingWebhookConfiguration{},
		},
		{
			name: "one vs zero webhooks",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "duplicate webhooks",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "different versions",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1beta1.SchemeGroupVersion),
				},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
					toWebhook(rbacv1beta1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "different groups",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(corev1.SchemeGroupVersion),
				},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(corev1.SchemeGroupVersion),
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "Overwrite FailurePolicy to Ignore (left)",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toFailWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "Overwrite FailurePolicy to Ignore (right)",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toFailWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		// Invalid Webhooks
		{
			name: "webhook missing Rules",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{{
					Rules: []admissionv1.RuleWithOperations{
						ruleFor(rbacv1.SchemeGroupVersion),
					},
					ObjectSelector: selectorFor(rbacv1.SchemeGroupVersion.Version),
				}},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{{}},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "drop webhook missing APIGroup",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{{
					Rules: []admissionv1.RuleWithOperations{{
						Rule: admissionv1.Rule{
							APIVersions: []string{"v1"},
							Resources:   []string{"namespaces"},
						},
					}},
				}},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "drop webhook missing APIVersion",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{{
					Rules: []admissionv1.RuleWithOperations{
						ruleFor(corev1.SchemeGroupVersion),
					},
				}},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "drop extra rules",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{{
					Rules: []admissionv1.RuleWithOperations{
						ruleFor(corev1.SchemeGroupVersion),
						ruleFor(rbacv1.SchemeGroupVersion),
					},
					ObjectSelector: selectorFor(corev1.SchemeGroupVersion.Version),
				}},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{{
					Rules: []admissionv1.RuleWithOperations{
						ruleFor(corev1.SchemeGroupVersion),
						ruleFor(rbacv1.SchemeGroupVersion),
					},
					ObjectSelector: selectorFor(corev1.SchemeGroupVersion.Version),
				}},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(corev1.SchemeGroupVersion),
				},
			},
		},
		{
			name: "drop duplicate rules",
			left: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			right: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
			want: &admissionv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionv1.ValidatingWebhook{
					toWebhook(rbacv1.SchemeGroupVersion),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Merge(tc.left, tc.right)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatal(diff)
			}

			got = Merge(tc.right, tc.left)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("not symmetric: %s", diff)
			}
		})
	}
}

func toFailWebhook(gv schema.GroupVersion) admissionv1.ValidatingWebhook {
	result := toWebhook(gv)
	// Go doesn't allow taking the address of constants.
	fail := admissionv1.Fail
	result.FailurePolicy = &fail
	return result
}
