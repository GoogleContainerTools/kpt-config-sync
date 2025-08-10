# Support for kpt Apply-Time-Mutation (ATM) in the Remediator

**Authors:**  
Roland Otta <roland.otta@viesure.io>

**Status:** Draft

## Background

kpt’s [Apply-Time-Mutation (ATM)](https://googlecontainertools.github.io/kpt/guides/apply/apply-time-mutation/) feature allows users to declare resource mutations that are resolved at apply time, enabling dynamic configuration based on cluster state or other resources.  
Currently, kpt-config-sync’s applier honors ATM annotations, but the remediator (which enforces declared state in the cluster) does not. This leads to a situation where resources with ATM annotations are initially applied with substitutions, but are later reverted by the remediator to their unmutated (placeholder) state.

## Motivation

- **Consistency:** Users expect that resources with ATM annotations remain mutated in the cluster, not reverted to their pre-mutation state.
- **Correctness:** Without ATM support in the remediator, Config Sync can break workloads that depend on ATM substitutions.
- **User Experience:** This change aligns the remediator’s behavior with the applier, reducing confusion and support burden.

This need is also reflected in the broader Kubernetes ecosystem. For example, [k8s-config-connector Issue #101](https://github.com/GoogleCloudPlatform/k8s-config-connector/issues/101) highlights a common user requirement: referencing values from other resources at apply time. In that issue, users request a generic mechanism to inject values from one resource into another, such as referencing a generated IP address value in a different resource (for a DNS record for example). ATM integration provides a standard, declarative solution for these scenarios, eliminating the need for custom controllers or ad-hoc scripting and making resource dependencies easier to manage and reason about.

## Goals

- Ensure the remediator applies ATM substitutions before applying or updating resources.
- Maintain parity between the applier and remediator logic for ATM.

## Non-Goals

- Changing the ATM mutator logic itself.
- Supporting ATM for resource types not currently supported by kpt.
- Changing the behavior of resources without ATM annotations.

## Proposal

- **Code Changes:**
  - Add a `restMapper` field to the remediator’s `clientApplier` struct.
  - Initialize the RESTMapper in the constructor using the discovery client.
  - In both `Create` and `Update` methods of the remediator, invoke the ATM mutator on the intended resource state before applying it.
  - Log mutation results and errors for observability.
- **Behavior:**  
  - When a resource with an ATM annotation is reconciled, the remediator will resolve and substitute placeholders using the ATM mutator, just as the applier does.
  - If mutation fails, the error is logged and surfaced in status.

## Alternatives Considered

- **Do nothing:** Not acceptable, as it breaks user expectations and workloads.
- **Custom mutation logic:** Rejected in favor of using the standard kpt ATM mutator for maintainability and consistency.

## Risks and Mitigations

- **Performance:**  
  - Risk: Additional mutation step may impact performance.
  - Mitigation: Mutation is only performed if ATM annotation is present.
- **Error Handling:**  
  - Risk: Mutation errors could block resource application.
  - Mitigation: Errors are logged and surfaced; resources without ATM are unaffected.
- **Compatibility:**  
  - Risk: Change in behavior for resources with ATM annotations.
  - Mitigation: This is the intended and correct behavior.

## Testing Plan

- Add unit tests for the remediator’s `Create` and `Update` methods with ATM-annotated resources.
- Add integration tests to verify that resources with ATM annotations are mutated and applied as expected.
- Manual testing with sample resources using ATM.

## Rollout/Upgrade Plan

- No migration or special rollout steps required.
- Change is backward compatible for users not using ATM.

## References

- [kpt Apply-Time-Mutation documentation](https://kpt.dev/reference/annotations/apply-time-mutation/)
- [k8s-config-connector Issue #101: Reference values from other resources](https://github.com/GoogleCloudPlatform/k8s-config-connector/issues/101)
