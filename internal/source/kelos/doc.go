// Package kelos reads Kelos Tasks and TaskRecords from the Kubernetes API
// and turns them into model runs, each identified by cluster, namespace and
// Task UID, never by Task name or branch. It is read-only: it never creates,
// updates or deletes Kubernetes objects.
package kelos
