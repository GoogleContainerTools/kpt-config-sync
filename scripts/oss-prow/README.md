
# Prow cluster setup

## Follow instructions at prow onboarding

Project `oss-prow-build-kpt-config-sync` has been set up, so there will be some
places that you will find ACLs that potentially collide with these instructions
and you will likely need to add permission for a new GCP SA.

## Set up perms for "Prow Pod Utilities"

Assign the Roles:

- `Editor`
- `Storage Object Viewer`

For some reason, it fails and complains about not having storage.objects.get,
but adding `Storage Object Viewer` doesn't fix this.  Adding `Editor` fixes, so
it's not clear what perms we need to give it.

## Rotate "prober-runner" secret

The prober cred is a service account key, which expires every 90 days.
A periodic job is set up to rotate the service account key for prober-runner and update the Kubernetes Secret on the Prow cluster.

You can also manually rotate the service account key by running `make manual-rotate-prober-sa-key-oss`.
