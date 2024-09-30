/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/slack-go/slack"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/ByeongHunKim/Istio-monitor-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// IstioMonitorReconciler reconciles a IstioMonitor object
type IstioMonitorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=monitoring.istio-ops.meiko.co.kr,resources=istiomonitors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.istio-ops.meiko.co.kr,resources=istiomonitors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.istio-ops.meiko.co.kr,resources=istiomonitors/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices;destinationrules;gateways,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch

func (r *IstioMonitorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var istioMonitor monitoringv1alpha1.IstioMonitor
	if err := r.Get(ctx, req.NamespacedName, &istioMonitor); err != nil {
		log.Error(err, "Unable to fetch IstioMonitor")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	for _, resourceType := range istioMonitor.Spec.ResourceTypes {
		if err := r.monitorIstioResource(ctx, resourceType, &istioMonitor); err != nil {
			log.Error(err, "Failed to monitor Istio resource", "resourceType", resourceType)
			return ctrl.Result{}, err
		}
	}

	istioMonitor.Status.LastNotificationTime = metav1.Now()
	if err := r.Status().Update(ctx, &istioMonitor); err != nil {
		log.Error(err, "Unable to update IstioMonitor status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *IstioMonitorReconciler) monitorIstioResource(ctx context.Context, resourceType string, istioMonitor *monitoringv1alpha1.IstioMonitor) error {
	log := log.FromContext(ctx)
	log.Info("Monitoring Istio resource", "resourceType", resourceType)

	gvk := schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1alpha3",
		Kind:    resourceType,
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)
	if err := r.List(ctx, list); err != nil {
		return fmt.Errorf("failed to list %s: %w", resourceType, err)
	}

	for _, item := range list.Items {
		lastModified := item.GetCreationTimestamp()
		if lastModified.After(istioMonitor.Status.LastNotificationTime.Time) {
			message := fmt.Sprintf("%s changed: %s in namespace %s", resourceType, item.GetName(), item.GetNamespace())
			log.Info("Detected change in Istio resource", "resourceType", resourceType, "name", item.GetName(), "namespace", item.GetNamespace())
			if err := r.sendSlackNotification(istioMonitor.Spec.SlackWebhookURL, message); err != nil {
				log.Error(err, "Failed to send Slack notification", "message", message)
			}
		}
	}

	return nil
}

func (r *IstioMonitorReconciler) sendSlackNotification(webhookURL, message string) error {
	msg := slack.WebhookMessage{
		Text: message,
	}
	return slack.PostWebhook(webhookURL, &msg)
}

// SetupWithManager sets up the controller with the Manager.
func (r *IstioMonitorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.IstioMonitor{}).
		Complete(r)
}
