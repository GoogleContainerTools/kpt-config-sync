This directory includes the configs for kubevirt in hierarchical format.

The example is used to:
1) test `nomos hydrate` generates the correct output;
2) verify that Config Sync can apply resources with known scopes instead of applying nothing when resources with unknown scope are encountered (design doc: go/config-sync-apply-scoped-resources).
