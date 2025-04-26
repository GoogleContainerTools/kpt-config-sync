# Config Sync Error Monitoring and Alerting Solution

A comprehensive solution for monitoring, logging, and alerting on Config Sync status errors. This solution includes a Kubernetes controller that watches `RootSync` and `RepoSync` resources, detects errors in their status fields, and integrates with Google Cloud services to provide enhanced observability and automated notifications.

The solution enables:
- Real-time monitoring of sync errors in Config Sync resources
- Structured logging to Google Cloud Logging
- Automated alerting through Pub/Sub, Cloud Functions, and email notifications
- Integration with various notification channels (email, Slack, etc.)
- Custom error handling workflows with Google Application Integration

> **Note**: ⚠️ **This component is not released with Config Sync**.
> - It is **only compatible** with the **Config Sync API at same branch HEAD**.
> - **Backward compatibility is not supported at this moment**.
> - **Version management and continuous builds** of this controller **must be handled by the user**.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
  - [Build and Deployment](#build-and-deployment)
  - [Verifying Deployment](#verifying-deployment)
  - [Sample Output](#sample-output)
- [Google Cloud Logging Integration](#google-cloud-logging-integration)
  - [Setting Up Permissions](#setting-up-permissions)
  - [Querying Logs in Google Cloud Logging](#querying-logs-in-google-cloud-logging)
  - [Creating Log Sinks](#creating-log-sinks)
- [Setting Up Alerting with Pub/Sub and Cloud Functions](#setting-up-alerting-with-pubsub-and-cloud-functions)
  - [Create a Pub/Sub Topic](#create-a-pubsub-topic)
  - [Create a Log Sink for Pub/Sub](#create-a-log-sink-for-pubsub)
  - [Create a Cloud Function for Email Alerts](#create-a-cloud-function-for-email-alerts)
- [Using Google Application Integration](#using-google-application-integration)
  - [Setting Up Integration](#setting-up-integration)
  - [Create an Integration for Email Notifications](#create-an-integration-for-email-notifications)
- [Cleanup](#cleanup)

## Prerequisites

- Go 1.21+
- Docker
- kubectl
- Access to a Kubernetes cluster with Config Sync installed and configured
- **Container registry (e.g., Google Artifact Registry)**
  *This guide assumes you have a registry configured and authenticated.*
- For Google Cloud integration:
  - Google Cloud Project with appropriate permissions
  - gcloud CLI installed and configured
  - Google Kubernetes Engine (GKE) cluster with Workload Identity (recommended)

## Quick Start

### Build and Deployment

1. **Configure environment variables**:

```sh
export REGION="us-central1"                # Your registry region
export PROJECT_ID="your-project-id"        # Your GCP project ID
export GAR_REPO_NAME="sync-status-watch"   # Name of your GAR repository
export IMAGE_NAME="sync-status-watch"      # Docker image name
export IMAGE_TAG="latest"                  # Docker image tag
```

2. **Create GAR repository** (if not already created):

```sh
gcloud artifacts repositories create ${GAR_REPO_NAME} \
  --repository-format=docker \
  --location=${REGION} \
  --description="Repository for Sync Status Watch Controller"
```

3. **Build and push the image** using the provided `Makefile`:

```sh
make build push
```

4. **Deploy to Kubernetes**:

```sh
# Create the namespace for the controller
kubectl create namespace sync-status-watch

# Deploy using the make target which handles image substitution in the manifest 
make deploy
```

> **Note**: The `make deploy` command automates the deployment process. Under the hood, it performs something like:
> ```sh
> sed "s|SYNC_STATUS_WATCH_CONTROLLER_IMAGE|${REGION}-docker.pkg.dev/${PROJECT_ID}/${GAR_REPO_NAME}/${IMAGE_NAME}:${IMAGE_TAG}|g" sync-watch-manifest.yaml | kubectl apply -f -
> ```
> See the `Makefile` for more details on the implementation.

### Verifying Deployment

Check that the controller is running:

```sh
kubectl get pods -n sync-status-watch
```

View the controller logs:

```sh
kubectl logs -f deployment/sync-status-watch -n sync-status-watch
```

### Sample Output

Successful startup:
```
{"level":"info","ts":1744307308.0837288,"caller":"/main.go:246","msg":"Starting standalone SyncStatusController"}
```

Error detected:
```
{"level":"info","ts":1744307308.1975124,"caller":"/main.go:119","msg":"Sync error detected","sync":"rs-quickstart","namespace":"config-management-system","kind":"RootSync","commit":"e79b57c3705821b2734531aec690e61a369c3586","status":"failed","error":"aggregated errors: Code: 1029, Message: KNV1029: Namespace-scoped configs of the same Group and Kind MUST have unique names if they are in the same Namespace. Found 2 configs of GroupKind \"ConfigMap\" in Namespace \"config-management-monitoring\" named \"test-monitoring-cm2\". Rename or delete the duplicates to fix:\n\nsource: config-sync-quickstart/multirepo/root/monitoring-cm2-dupe.yaml\nnamespace: config-management-monitoring\nmetadata.name: test-monitoring-cm2\ngroup:\nversion: v1\nkind: ConfigMap\n\nsource: config-sync-quickstart/multirepo/root/monitoring-cm2.yaml\nnamespace: config-management-monitoring\nmetadata.name: test-monitoring-cm2\ngroup:\nversion: v1\nkind: ConfigMap\n\nFor more information, see https://g.co/cloud/acm-errors#knv1029"}
```

## Google Cloud Logging Integration

The controller outputs structured logs that are automatically collected in Google Cloud Logging when running on GKE.

> **Note**: When running in a GKE cluster, logs from the controller pods are automatically collected and sent to Google Cloud Logging without any additional configuration. This works both for clusters with or without Workload Identity Federation enabled. All structured logs will be available in Cloud Logging with the appropriate Kubernetes resource labels.

### Querying Logs in Google Cloud Logging

To view logs in the Google Cloud Console:

1. Go to [Logs Explorer](https://console.cloud.google.com/logs/query)
2. Use the following query to filter logs from the controller:

```
resource.type="k8s_container"
resource.labels.namespace_name="sync-status-watch"
resource.labels.container_name="sync-status-watch"
```

To filter for only error logs:

```
resource.type="k8s_container"
resource.labels.namespace_name="sync-status-watch"
resource.labels.container_name="sync-status-watch"
jsonPayload.msg="Sync error detected"
```

Using the gcloud CLI:

```sh
gcloud logging read 'resource.type="k8s_container" AND resource.labels.namespace_name="sync-status-watch" AND jsonPayload.msg="Sync error detected"' \
  --project=${PROJECT_ID} \
  --format=json \
  --limit=10
```

### Creating Log Sinks

To export logs to other destinations, create a log sink:

```sh
# Create a sink to a Cloud Storage bucket
export SINK_NAME="sync-status-errors"
export BUCKET_NAME="sync-status-logs-${PROJECT_ID}"

# Create the storage bucket
gsutil mb -l ${REGION} gs://${BUCKET_NAME}

# Create the log sink
gcloud logging sinks create ${SINK_NAME} storage.googleapis.com/${BUCKET_NAME} \
  --log-filter='resource.type="k8s_container" AND resource.labels.namespace_name="sync-status-watch" AND jsonPayload.msg="Sync error detected"'

# Get the service account created for the sink
export SINK_SA=$(gcloud logging sinks describe ${SINK_NAME} --format='value(writerIdentity)')

# Grant permissions to write to the bucket
gsutil iam ch ${SINK_SA}:roles/storage.objectCreator gs://${BUCKET_NAME}
```

## Setting Up Alerting with Pub/Sub and Cloud Functions

### Create a Pub/Sub Topic

```sh
export TOPIC_NAME="sync-error-notifications"

# Create the topic
gcloud pubsub topics create ${TOPIC_NAME}
```

### Create a Log Sink for Pub/Sub

```sh
export PUBSUB_SINK_NAME="sync-errors-to-pubsub"

# Create log sink that publishes to Pub/Sub
gcloud logging sinks create ${PUBSUB_SINK_NAME} pubsub.googleapis.com/projects/${PROJECT_ID}/topics/${TOPIC_NAME} \
  --log-filter='resource.type="k8s_container" AND resource.labels.namespace_name="sync-status-watch" AND jsonPayload.msg="Sync error detected"'

# Get the service account created for the sink
export PUBSUB_SINK_SA=$(gcloud logging sinks describe ${PUBSUB_SINK_NAME} --format='value(writerIdentity)')

# Grant permissions to publish to the topic
gcloud pubsub topics add-iam-policy-binding ${TOPIC_NAME} \
  --member=${PUBSUB_SINK_SA} \
  --role=roles/pubsub.publisher
```

### Create a Cloud Function for Email Alerts

1. **Create a directory for the Cloud Function**:

```sh
mkdir -p cloud-function && cd cloud-function
```

2. **Create function.go**:

```sh
cat << 'EOF' > function.go
package function

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/smtp"
	"os"
)

// PubSubMessage is the payload of a Pub/Sub event
type PubSubMessage struct {
	Data []byte `json:"data"`
}

// LogEntry represents the structure of Cloud Logging entries
type LogEntry struct {
	JsonPayload struct {
		Sync      string `json:"sync"`
		Namespace string `json:"namespace"`
		Kind      string `json:"kind"`
		Commit    string `json:"commit"`
		Error     string `json:"error"`
		Msg       string `json:"msg"`
	} `json:"jsonPayload"`
	Resource struct {
		Labels struct {
			NamespaceName string `json:"namespace_name"`
			PodName       string `json:"pod_name"`
		} `json:"labels"`
	} `json:"resource"`
	Timestamp string `json:"timestamp"`
}

// SyncErrorAlert is triggered by a Pub/Sub message
func SyncErrorAlert(ctx context.Context, m PubSubMessage) error {
	emailFrom := os.Getenv("EMAIL_FROM")
	emailTo := os.Getenv("EMAIL_TO")
	password := os.Getenv("EMAIL_PASSWORD")
	projectID := os.Getenv("PROJECT_ID")

	if emailFrom == "" || emailTo == "" || password == "" {
		return fmt.Errorf("missing required environment variables")
	}

	// Decode the Pub/Sub message
	var pubsubMessage struct {
		Message struct {
			Data string `json:"data"`
		} `json:"message"`
	}

	// If direct invocation from Pub/Sub
	if err := json.Unmarshal(m.Data, &pubsubMessage); err != nil {
		log.Printf("Parsing as direct Pub/Sub message: %v", err)
	}

	var logData []byte
	var err error
	if pubsubMessage.Message.Data != "" {
		// Decode the base64 data in the Pub/Sub message
		logData, err = base64.StdEncoding.DecodeString(pubsubMessage.Message.Data)
		if err != nil {
			log.Printf("Error decoding base64 data: %v", err)
			return err
		}
	} else {
		// If no wrapper, use the data directly
		logData = m.Data
	}

	// Parse the log entry
	var logEntry LogEntry
	if err := json.Unmarshal(logData, &logEntry); err != nil {
		log.Printf("Error unmarshaling log entry: %v", err)
		log.Printf("Raw log data: %s", string(logData))
		return err
	}

	// Check if we have the required fields
	if logEntry.JsonPayload.Msg != "Sync error detected" || 
	   logEntry.JsonPayload.Sync == "" || 
	   logEntry.JsonPayload.Error == "" {
		log.Printf("Incomplete log data, skipping email alert")
		log.Printf("Payload: %+v", logEntry.JsonPayload)
		return nil
	}

	// Prepare email content
	subject := fmt.Sprintf("Config Sync Error: %s %s/%s", 
		logEntry.JsonPayload.Kind,
		logEntry.JsonPayload.Namespace, 
		logEntry.JsonPayload.Sync)

	body := fmt.Sprintf(`
A Config Sync error has been detected:

- Resource: %s %s/%s
- Commit: %s
- Error: %s
- Time: %s

Please investigate this issue in the Google Cloud Console.
Logs URL: https://console.cloud.google.com/logs/query?project=%s
`,
		logEntry.JsonPayload.Kind,
		logEntry.JsonPayload.Namespace,
		logEntry.JsonPayload.Sync,
		logEntry.JsonPayload.Commit,
		logEntry.JsonPayload.Error,
		logEntry.Timestamp,
		projectID)

	// Set up authentication
	auth := smtp.PlainAuth("", emailFrom, password, "smtp.gmail.com")

	// Compose the message
	msg := []byte("To: " + emailTo + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" + body)

	// Send the email
	err = smtp.SendMail("smtp.gmail.com:587", auth, emailFrom, []string{emailTo}, msg)
	if err != nil {
		log.Printf("Failed to send email: %v", err)
		return err
	}

	log.Printf("Email alert sent to %s", emailTo)
	return nil
}
EOF
```

3. **Create go.mod with required dependencies**:

```sh
go mod init configsync.gke.io/sync-error-function && go mod tidy
```

4. **Configure Pub/Sub to Cloud Function Authentication**:

```sh
# Create Google Service Account used by Pub/Sub to authenticate when pushing messages to your Cloud Function.
export SA_NAME=pubsub-cloud-function-invoker
export FUNCTION_NAME=sync-error-alert
gcloud iam service-accounts create ${SA_NAME} \
  --display-name="Pub/Sub Invoker"

# Assign the Cloud Functions Invoker role to the service account for your specific Cloud Function.
gcloud functions add-iam-policy-binding ${FUNCTION_NAME} \
  --member="serviceAccount:${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/cloudfunctions.invoker"

# Get the Pub/Sub service account
export PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format="value(projectNumber)")
export PUBSUB_SERVICE_ACCOUNT="service-${PROJECT_NUMBER}@gcp-sa-pubsub.iam.gserviceaccount.com"
export SUBSCRIPTION_NAME="sync-error-alert-subscription"

# Grant the Pub/Sub service the Service Account Token Creator role on your service account.
# This allows Pub/Sub to generate authentication tokens on behalf of the service account.​
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
  --member="serviceAccount:${PUBSUB_SERVICE_ACCOUNT}" \
  --role="roles/iam.serviceAccountTokenCreator"

# Set up a push subscription that uses the service account for authentication.
gcloud pubsub subscriptions create ${SUBSCRIPTION_NAME} \
  --topic=${TOPIC_NAME} \
  --push-endpoint=https://${REGION}-${PROJECT_ID}.cloudfunctions.net/${FUNCTION_NAME} \
  --push-auth-service-account=${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com
```

Note: For Gmail, you'll need to use an [App Password](https://support.google.com/accounts/answer/185833) instead of your regular password. Make sure your Gmail account has 2FA enabled to generate an App Password.

5. **Deploy the Cloud Function**:

```sh
gcloud functions deploy ${FUNCTION_NAME} \
  --gen2 \
  --runtime=go121 \
  --region=${REGION} \
  --source=. \
  --entry-point=SyncErrorAlert \
  --trigger-topic=${TOPIC_NAME} \
  --set-env-vars=EMAIL_FROM=your-email-from@example.com,EMAIL_TO=your-email-to@example.com,EMAIL_PASSWORD=your-app-password,PROJECT_ID=${PROJECT_ID} \
  --service-account=${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com
```

## Using Google Application Integration

As an alternative to Cloud Functions, you can use [Google Application Integration](https://cloud.google.com/application-integration/docs/overview) to build no-code/low-code integrations that respond to Pub/Sub messages and send notifications through various channels.

### Setting Up Integration

1. **Enable the Application Integration API**:

```sh
gcloud services enable integrations.googleapis.com
```

2. **Create an integration**:

Navigate to the [Application Integration Console](https://console.cloud.google.com/integrations) in your Google Cloud Project and follow these steps:

### Create an Integration for Email Notifications

1. **Create a new integration**:
   - Go to the Application Integration section in the Google Cloud Console
   - Click "Create Integration"
   - Name your integration (e.g., "SyncErrorNotification")
   - Choose a region (same as your other resources)
   - Click "Create"

2. **Add a Pub/Sub trigger**:
   - In the integration editor, add a Pub/Sub trigger
   - Select the topic you created earlier (e.g., "sync-error-notifications")
   - Configure the trigger to process one message at a time

3. **Parse the Pub/Sub message**:
   - Add a "Data Mapping" task
   - Create variables to extract the log entry information:
     - Extract and decode the base64 message
     - Parse the JSON to access sync name, namespace, error details, etc.

4. **Add email notification task**:
   - Add a "Gmail" or "SMTP" connector (available in the integration marketplace)
   - Configure with your email credentials
   - Map the parsed data to email fields (subject, body, recipients)

5. **Publish and test your integration**:
   - Save and publish your integration
   - Test it with a sample error from Config Sync

For detailed instructions on creating integrations with Pub/Sub triggers and email notifications, refer to the [official Google Application Integration documentation](https://cloud.google.com/application-integration/docs/create-trigger-pubsub).

You can also set up integrations with other systems like:
- Slack for team notifications
- Jira for ticket creation
- ServiceNow for incident management
- Webhook callouts to custom systems

## Cleanup

To remove all resources created in this guide:

```sh
# Remove Kubernetes resources
kubectl delete -f sync-watch-manifest.yaml
kubectl delete namespace sync-status-watch

# Remove Pub/Sub topic and subscription
gcloud pubsub subscriptions delete ${SUBSCRIPTION_NAME}
gcloud pubsub topics delete ${TOPIC_NAME}

# Remove log sinks
gcloud logging sinks delete ${PUBSUB_SINK_NAME}
gcloud logging sinks delete ${SINK_NAME}

# Remove Cloud Function
gcloud functions delete ${FUNCTION_NAME} --region=${REGION}

# Remove IAM bindings
gcloud iam service-accounts remove-iam-policy-binding \
  ${LOG_GSA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com \
  --role="roles/iam.workloadIdentityUser" \
  --member="serviceAccount:${PROJECT_ID}.svc.id.goog[${KSA_NAMESPACE}/${KSA_NAME}]"

# Remove service accounts
gcloud iam service-accounts delete ${LOG_GSA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com
gcloud iam service-accounts delete ${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com

# Delete the storage bucket (if created)
gsutil rm -r gs://${BUCKET_NAME}

# Delete GAR repository (optional)
gcloud artifacts repositories delete ${GAR_REPO_NAME} --location=${REGION}
```