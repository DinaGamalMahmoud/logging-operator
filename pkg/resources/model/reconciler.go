// Copyright © 2019 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"emperror.dev/errors"
	"github.com/cisco-open/operator-tools/pkg/secret"
	"github.com/cisco-open/operator-tools/pkg/utils"
	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kube-logging/logging-operator/pkg/resources/configcheck"
	loggingv1beta1 "github.com/kube-logging/logging-operator/pkg/sdk/logging/api/v1beta1"

	"github.com/kube-logging/logging-operator/pkg/mirror"
)

func NewValidationReconciler(
	repo client.StatusClient,
	resources LoggingResources,
	secrets SecretLoaderFactory,
	logger logr.Logger,
) func(ctx context.Context) (*reconcile.Result, error) {
	return func(ctx context.Context) (*reconcile.Result, error) {
		var patchRequests []patchRequest
		registerForPatching := func(obj client.Object) {
			patchRequests = append(patchRequests, patchRequest{
				Obj:   obj,
				Patch: client.MergeFrom(obj.DeepCopyObject().(client.Object)),
			})
		}

		for i := range resources.Fluentd.ClusterOutputs {
			output := &resources.Fluentd.ClusterOutputs[i]
			registerForPatching(output)

			output.Status.Active = utils.BoolPointer(false)
			output.Status.Problems = nil

			if output.Name == resources.Logging.Spec.ErrorOutputRef {
				output.Status.Active = utils.BoolPointer(true)
			}

			output.Status.Problems = append(output.Status.Problems,
				validateOutputSpec(output.Spec.OutputSpec, secrets.OutputSecretLoaderForNamespace(output.Namespace))...)
			output.Status.ProblemsCount = len(output.Status.Problems)
		}

		for i := range resources.Fluentd.Outputs {
			output := &resources.Fluentd.Outputs[i]
			registerForPatching(output)

			output.Status.Active = utils.BoolPointer(false)
			output.Status.Problems = nil

			output.Status.Problems = append(output.Status.Problems,
				validateOutputSpec(output.Spec, secrets.OutputSecretLoaderForNamespace(output.Namespace))...)
			output.Status.ProblemsCount = len(output.Status.Problems)
		}

		for i := range resources.SyslogNG.ClusterOutputs {
			output := &resources.SyslogNG.ClusterOutputs[i]
			registerForPatching(output)

			output.Status.Active = utils.BoolPointer(false)
			output.Status.Problems = nil

			if output.Name == resources.Logging.Spec.ErrorOutputRef {
				output.Status.Active = utils.BoolPointer(true)
			}

			output.Status.Problems = append(output.Status.Problems,
				validateOutputSpec(output.Spec.SyslogNGOutputSpec, secrets.OutputSecretLoaderForNamespace(output.Namespace))...)
			output.Status.ProblemsCount = len(output.Status.Problems)
		}

		for i := range resources.SyslogNG.Outputs {
			output := &resources.SyslogNG.Outputs[i]
			registerForPatching(output)

			output.Status.Active = utils.BoolPointer(false)
			output.Status.Problems = nil

			output.Status.Problems = append(output.Status.Problems,
				validateOutputSpec(output.Spec, secrets.OutputSecretLoaderForNamespace(output.Namespace))...)
			output.Status.ProblemsCount = len(output.Status.Problems)
		}

		for i := range resources.Fluentd.ClusterFlows {
			flow := &resources.Fluentd.ClusterFlows[i]
			registerForPatching(flow)

			flow.Status.Active = utils.BoolPointer(false)
			flow.Status.Problems = nil

			if len(flow.Spec.GlobalOutputRefs) == 0 && len(flow.Spec.OutputRefs) > 0 {
				flow.Status.Problems = append(flow.Status.Problems, "\"outputRefs\" field is deprecated, use \"globalOutputRefs\" instead")
			}

			for _, ref := range flow.Spec.GlobalOutputRefs {
				if output := resources.Fluentd.ClusterOutputs.FindByName(ref); output != nil {
					flow.Status.Active = utils.BoolPointer(true)
					output.Status.Active = utils.BoolPointer(true)
				} else {
					flow.Status.Problems = append(flow.Status.Problems, fmt.Sprintf("dangling global output reference: %s", ref))
				}
			}
			flow.Status.ProblemsCount = len(flow.Status.Problems)
		}

		for i := range resources.Fluentd.Flows {
			flow := &resources.Fluentd.Flows[i]
			registerForPatching(flow)

			flow.Status.Active = utils.BoolPointer(false)
			flow.Status.Problems = nil

			if len(flow.Spec.LocalOutputRefs)+len(flow.Spec.GlobalOutputRefs) == 0 && len(flow.Spec.OutputRefs) > 0 {
				flow.Status.Problems = append(flow.Status.Problems, "\"outputRefs\" field is deprecated, use \"globalOutputRefs\" and \"localOutputRefs\" instead")
			}

			for _, ref := range flow.Spec.GlobalOutputRefs {
				if output := resources.Fluentd.ClusterOutputs.FindByName(ref); output != nil {
					flow.Status.Active = utils.BoolPointer(true)
					output.Status.Active = utils.BoolPointer(true)
				} else {
					flow.Status.Problems = append(flow.Status.Problems, fmt.Sprintf("dangling global output reference: %s", ref))
				}
			}

			for _, ref := range flow.Spec.LocalOutputRefs {
				if output := resources.Fluentd.Outputs.FindByNamespacedName(flow.Namespace, ref); output != nil {
					flow.Status.Active = utils.BoolPointer(true)
					output.Status.Active = utils.BoolPointer(true)
				} else {
					flow.Status.Problems = append(flow.Status.Problems, fmt.Sprintf("dangling local output reference: %s", ref))
				}
			}
			flow.Status.ProblemsCount = len(flow.Status.Problems)
		}

		if resources.Fluentd.Configuration != nil {

		}
		if resources.Fluentd.Configuration != nil && resources.Logging.Spec.FluentdSpec != nil {
			resources.Logging.Status.Problems = append(resources.Logging.Status.Problems, fmt.Sprintf("Fluentd configuration reference set (name=%s), but inline fluentd configuration found is set as well, clearing inline", resources.Fluentd.Configuration.Name))
			resources.Logging.Spec.FluentdSpec = nil
		}

		if fluentd := resources.GetFluentd(); fluentd != nil {
			registerForPatching(fluentd)
		}

		for i := range resources.SyslogNG.ClusterFlows {
			flow := &resources.SyslogNG.ClusterFlows[i]
			registerForPatching(flow)

			flow.Status.Active = utils.BoolPointer(false)
			flow.Status.Problems = nil

			for _, ref := range flow.Spec.GlobalOutputRefs {
				if output := resources.SyslogNG.ClusterOutputs.FindByName(ref); output != nil {
					flow.Status.Active = utils.BoolPointer(true)
					output.Status.Active = utils.BoolPointer(true)
				} else {
					flow.Status.Problems = append(flow.Status.Problems, fmt.Sprintf("dangling global output reference: %s", ref))
				}
			}
			flow.Status.ProblemsCount = len(flow.Status.Problems)
		}

		for i := range resources.SyslogNG.Flows {
			flow := &resources.SyslogNG.Flows[i]
			registerForPatching(flow)

			flow.Status.Active = utils.BoolPointer(false)
			flow.Status.Problems = nil

			for _, ref := range flow.Spec.GlobalOutputRefs {
				if output := resources.SyslogNG.ClusterOutputs.FindByName(ref); output != nil {
					flow.Status.Active = utils.BoolPointer(true)
					output.Status.Active = utils.BoolPointer(true)
				} else {
					flow.Status.Problems = append(flow.Status.Problems, fmt.Sprintf("dangling global output reference: %s", ref))
				}
			}

			for _, ref := range flow.Spec.LocalOutputRefs {
				if output := resources.SyslogNG.Outputs.FindByNamespacedName(flow.Namespace, ref); output != nil {
					flow.Status.Active = utils.BoolPointer(true)
					output.Status.Active = utils.BoolPointer(true)
				} else {
					flow.Status.Problems = append(flow.Status.Problems, fmt.Sprintf("dangling local output reference: %s", ref))
				}
			}
			flow.Status.ProblemsCount = len(flow.Status.Problems)
		}

		registerForPatching(&resources.Logging)
		resources.Logging.Status.Problems = nil
		resources.Logging.Status.WatchNamespaces = nil

		if !resources.Logging.WatchAllNamespaces() {
			resources.Logging.Status.WatchNamespaces = resources.WatchNamespaces
		}

		if resources.Logging.Spec.WatchNamespaceSelector != nil &&
			len(resources.Logging.Status.WatchNamespaces) == 0 {
			resources.Logging.Status.Problems = append(resources.Logging.Status.Problems, "Defined watchNamespaceSelector did not match any namespaces")
		}

		loggingsForTheSameRef := make([]string, 0)
		for _, l := range resources.AllLoggings {
			if l.Name == resources.Logging.Name {
				continue
			}
			if l.Spec.LoggingRef == resources.Logging.Spec.LoggingRef {
				loggingsForTheSameRef = append(loggingsForTheSameRef, l.Name)
			}
		}

		if len(loggingsForTheSameRef) > 0 {
			problem := fmt.Sprintf("Deprecated behaviour! Other logging resources exist with the same loggingRef: %s. This is going to be an error with the next major release.",
				strings.Join(loggingsForTheSameRef, ","))
			logger.Info(fmt.Sprintf("WARNING %s", problem))
			resources.Logging.Status.Problems = append(resources.Logging.Status.Problems, problem)
		}

		for hash, r := range resources.Logging.Status.ConfigCheckResults {
			if !r {
				problem := fmt.Sprintf("Configuration with checksum %s has failed. "+
					"Config secrets: `kubectl get secret -n %s -l %s=%s`. "+
					"Configcheck pod log: `kubectl logs -n %s -l %s=%s --tail -1`",
					hash,
					resources.Logging.Spec.ControlNamespace, configcheck.HashLabel, hash,
					resources.Logging.Spec.ControlNamespace, configcheck.HashLabel, hash)
				resources.Logging.Status.Problems = append(resources.Logging.Status.Problems, problem)
			}
		}

		if len(resources.Logging.Spec.NodeAgents) > 0 || len(resources.NodeAgents) > 0 {
			// load agents from standalone NodeAgent resources and additionally with inline nodeAgents from the logging resource
			// for compatibility reasons
			agents := make(map[string]loggingv1beta1.NodeAgentConfig)
			for _, a := range resources.NodeAgents {
				agents[a.Name] = a.Spec.NodeAgentConfig
			}
			for _, a := range resources.Logging.Spec.NodeAgents {
				if _, exists := agents[a.Name]; !exists {
					agents[a.Name] = a.NodeAgentConfig
					problem := fmt.Sprintf("inline nodeAgent definition (%s) in Logging resource is deprecated, use standalone NodeAgent CRD instead!", a.Name)
					resources.Logging.Status.Problems = append(resources.Logging.Status.Problems, problem)
				} else {
					problem := fmt.Sprintf("NodeAgent resource overrides inline nodeAgent definition (%s) in Logging resource", a.Name)
					resources.Logging.Status.Problems = append(resources.Logging.Status.Problems, problem)
				}
			}
		}

		if resources.Logging.Spec.FluentbitSpec != nil && len(resources.LoggingRoutes) > 0 {
			resources.Logging.Status.Problems = append(resources.Logging.Status.Problems, "Logging routes are not supported for embedded fluentbit configs, please use a separate FluentbitAgent resource!")
		}

		slices.Sort(resources.Logging.Status.Problems)
		resources.Logging.Status.ProblemsCount = len(resources.Logging.Status.Problems)

		var errs error
		for _, req := range patchRequests {
			if req.IsEmptyPatch() {
				continue
			}

			obj := req.Obj.DeepCopyObject().(client.Object) // copy object so that the original is not changed by the call to Patch
			if err := repo.Status().Patch(ctx, obj, req.Patch); err != nil {
				errs = errors.Append(errs, err)
			}
		}

		return nil, errs
	}
}

func validateOutputSpec(spec interface{}, secrets secret.SecretLoader) (problems []string) {
	var configuredFields []string
	it := mirror.StructRange(spec)
	for it.Next() {
		if it.Field().Type.Kind() == reflect.Ptr && !it.Value().IsNil() {
			configuredFields = append(configuredFields, jsonFieldName(it.Field()))
			problems = append(problems, checkSecrets(it.Value().Elem(), secrets)...)
		}
	}

	switch len(configuredFields) {
	case 0:
		problems = append(problems, "no output target configured")
	case 1:
		// OK
	default:
		problems = append(problems, fmt.Sprintf("multiple output targets configured: %s", configuredFields))
	}
	return
}

func checkSecrets(v reflect.Value, secrets secret.SecretLoader) (problems []string) {
	switch v.Kind() {
	case reflect.Array, reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			problems = append(problems, checkSecrets(v.Index(i), secrets)...)
		}
	case reflect.Pointer:
		problems = checkSecrets(v.Elem(), secrets)
	case reflect.Struct:
		it := mirror.NewStructIter(v)
		for it.Next() {
			if s, _ := it.Value().Interface().(*secret.Secret); s != nil {
				if _, err := secrets.Load(s); err != nil {
					problems = append(problems, err.Error())
				}
			}
		}
	}
	return
}

type patchRequest struct {
	Obj   client.Object
	Patch client.Patch
}

func (r patchRequest) IsEmptyPatch() bool {
	data, err := r.Patch.Data(r.Obj)
	return err == nil && string(data) == "{}"
}

func jsonFieldName(f reflect.StructField) string {
	t := f.Tag.Get("json")
	n := strings.Split(t, ",")[0]
	if n != "" {
		return n
	}
	return f.Name
}
