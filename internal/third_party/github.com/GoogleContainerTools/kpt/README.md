# github.com/GoogleContainerTools/kpt

Vendored from https://github.com/GoogleContainerTools/kpt, to apply local patches.

Local modifications:
-  Update `pkg/live/inventoryrg.go`:
  - Fix `InventoryResourceGroup.Apply` & `ApplyWithPrune` to correctly update
    `status.observedGeneration` after updating the spec.
  - Change `InventoryResourceGroup` to expose the wrapped Unstructured object as
    a parent object, allowing direct access to the object getters and setters.
    This allows Config Sync to set inventory metadata before each apply.
  - Change `InventoryResourceGroup.Apply` & `ApplyWithPrune` to update the
    wrapped Unstructured object after each spec and status update.
    This allows Config Sync to read the last known inventory state without
    re-fetching it (excluding status updates from the ResourceGroup controller).
