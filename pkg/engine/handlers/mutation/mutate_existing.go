package mutation

import (
	"context"

	"github.com/go-logr/logr"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	engineapi "github.com/kyverno/kyverno/pkg/engine/api"
	"github.com/kyverno/kyverno/pkg/engine/handlers"
	"github.com/kyverno/kyverno/pkg/engine/internal"
	"github.com/kyverno/kyverno/pkg/engine/mutate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type mutateExistingHandler struct {
	client dclient.Interface
}

func NewMutateExistingHandler(
	client dclient.Interface,
) (handlers.Handler, error) {
	return mutateExistingHandler{
		client: client,
	}, nil
}

func (h mutateExistingHandler) Process(
	ctx context.Context,
	logger logr.Logger,
	policyContext engineapi.PolicyContext,
	resource unstructured.Unstructured,
	rule kyvernov1.Rule,
	contextLoader engineapi.EngineContextLoader,
) (unstructured.Unstructured, []engineapi.RuleResponse) {
	var responses []engineapi.RuleResponse
	logger.V(3).Info("processing mutate rule")
	targets, err := loadTargets(h.client, rule.Mutation.Targets, policyContext, logger)
	if err != nil {
		rr := internal.RuleError(rule, engineapi.Mutation, "", err)
		responses = append(responses, *rr)
	}

	for _, target := range targets {
		if target.unstructured.Object == nil {
			continue
		}
		policyContext := policyContext.Copy()
		if err := policyContext.JSONContext().AddTargetResource(target.unstructured.Object); err != nil {
			logger.Error(err, "failed to add target resource to the context")
			continue
		}
		// load target specific context
		if err := contextLoader(ctx, target.context, policyContext.JSONContext()); err != nil {
			rr := internal.RuleError(rule, engineapi.Mutation, "failed to load context", err)
			responses = append(responses, *rr)
			continue
		}
		// load target specific preconditions
		preconditionsPassed, err := internal.CheckPreconditions(logger, policyContext.JSONContext(), target.preconditions)
		if err != nil {
			rr := internal.RuleError(rule, engineapi.Mutation, "failed to evaluate preconditions", err)
			responses = append(responses, *rr)
			continue
		}
		if !preconditionsPassed {
			rr := internal.RuleSkip(rule, engineapi.Mutation, "preconditions not met")
			responses = append(responses, *rr)
			continue
		}

		// logger.V(4).Info("apply rule to resource", "resource namespace", patchedResource.unstructured.GetNamespace(), "resource name", patchedResource.unstructured.GetName())
		var mutateResp *mutate.Response
		if rule.Mutation.ForEachMutation != nil {
			m := &forEachMutator{
				rule:          rule,
				foreach:       rule.Mutation.ForEachMutation,
				policyContext: policyContext,
				resource:      target.resourceInfo,
				logger:        logger,
				contextLoader: contextLoader,
				nesting:       0,
			}
			mutateResp = m.mutateForEach(ctx)
		} else {
			mutateResp = mutate.Mutate(&rule, policyContext.JSONContext(), target.unstructured, logger)
		}
		if ruleResponse := buildRuleResponse(&rule, mutateResp, target.resourceInfo); ruleResponse != nil {
			responses = append(responses, *ruleResponse)
		}
	}
	return resource, responses
}
