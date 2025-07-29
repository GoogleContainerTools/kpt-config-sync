# Scheduling Config Sync system pods using MutatingAdmissionPolicies

[MutatingAdmissionPolicy] can be used to configure the way the Config Sync's
system Pods are scheduled.

The [example in this directory](./config-sync-node-placement.yaml) demonstrates
how to inject `nodeAffinity` into all Pods in the `config-management-system` Namespace.

Note that this is just a demonstrative example, and different `matchConstraints` and
`mutations` can be applied depending on the use case.

Caveats:

- MutatingAdmissionPolicy is currently in alpha and requires feature gate enablement. It is [targeted to enter beta in k8s 1.34].
- MutatingAdmissionPolicy does not update existing Pods. Pods created before the policy was applied must be updated/recreated.




[MutatingAdmissionPolicy]: https://kubernetes.io/docs/reference/access-authn-authz/mutating-admission-policy/
[targeted to enter beta in k8s 1.34]: https://github.com/kubernetes/enhancements/issues/3962
