# Notifications

`dataprotection` v2 ships the notification gateway as part of the core installer.

When you run:

```bash
./data-protection-operator-amd64.run install -y
```

the installer deploys both:

- `data-protection-operator-controller-manager`
- `data-protection-notification-gateway`

Use [06-notificationendpoint.yaml](C:/Users/admin/Desktop/release/dataprotection/config/samples/quickstart/06-notificationendpoint.yaml) to register outbound webhook targets, then attach them from:

- `BackupPolicy.spec.notificationRefs`
- `BackupJob.spec.notificationRefs`
- `RestoreJob.spec.notificationRefs`

Event types currently emitted by the controller:

- `BackupSucceeded`
- `BackupFailed`
- `RestoreSucceeded`
- `RestoreFailed`
- `StorageProbeFailed`
- `RetentionPruneFailed`

The gateway itself is not a storage or backup addon. It is part of the control plane package.
