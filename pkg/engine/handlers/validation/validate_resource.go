package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	gojmespath "github.com/jmespath/go-jmespath"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	engineapi "github.com/kyverno/kyverno/pkg/engine/api"
	"github.com/kyverno/kyverno/pkg/engine/handlers"
	"github.com/kyverno/kyverno/pkg/engine/internal"
	engineutils "github.com/kyverno/kyverno/pkg/engine/utils"
	"github.com/kyverno/kyverno/pkg/engine/validate"
	"github.com/kyverno/kyverno/pkg/engine/variables"
	"github.com/kyverno/kyverno/pkg/utils/api"
	datautils "github.com/kyverno/kyverno/pkg/utils/data"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type validateResourceHandler struct{}

func NewValidateResourceHandler() (handlers.Handler, error) {
	return validateResourceHandler{}, nil
}

func (h validateResourceHandler) Process(
	ctx context.Context,
	logger logr.Logger,
	policyContext engineapi.PolicyContext,
	resource unstructured.Unstructured,
	rule kyvernov1.Rule,
	contextLoader engineapi.EngineContextLoader,
) (unstructured.Unstructured, []engineapi.RuleResponse) {
	v := newValidator(logger, contextLoader, policyContext, rule)
	return resource, handlers.RuleResponses(v.validate(ctx))
}

type validator struct {
	log              logr.Logger
	policyContext    engineapi.PolicyContext
	rule             kyvernov1.Rule
	contextEntries   []kyvernov1.ContextEntry
	anyAllConditions apiextensions.JSON
	pattern          apiextensions.JSON
	anyPattern       apiextensions.JSON
	deny             *kyvernov1.Deny
	forEach          []kyvernov1.ForEachValidation
	contextLoader    engineapi.EngineContextLoader
	nesting          int
}

func newValidator(log logr.Logger, contextLoader engineapi.EngineContextLoader, ctx engineapi.PolicyContext, rule kyvernov1.Rule) *validator {
	return &validator{
		log:           log,
		rule:          rule,
		policyContext: ctx,
		contextLoader: contextLoader,
		pattern:       rule.Validation.GetPattern(),
		anyPattern:    rule.Validation.GetAnyPattern(),
		deny:          rule.Validation.Deny,
		forEach:       rule.Validation.ForEachValidation,
	}
}

func newForEachValidator(
	foreach kyvernov1.ForEachValidation,
	contextLoader engineapi.EngineContextLoader,
	nesting int,
	rule kyvernov1.Rule,
	ctx engineapi.PolicyContext,
	log logr.Logger,
) (*validator, error) {
	anyAllConditions, err := datautils.ToMap(foreach.AnyAllConditions)
	if err != nil {
		return nil, fmt.Errorf("failed to convert ruleCopy.Validation.ForEachValidation.AnyAllConditions: %w", err)
	}
	nestedForEach, err := api.DeserializeJSONArray[kyvernov1.ForEachValidation](foreach.ForEachValidation)
	if err != nil {
		return nil, fmt.Errorf("failed to convert ruleCopy.Validation.ForEachValidation.AnyAllConditions: %w", err)
	}
	return &validator{
		log:              log,
		policyContext:    ctx,
		rule:             rule,
		contextLoader:    contextLoader,
		contextEntries:   foreach.Context,
		anyAllConditions: anyAllConditions,
		pattern:          foreach.GetPattern(),
		anyPattern:       foreach.GetAnyPattern(),
		deny:             foreach.Deny,
		forEach:          nestedForEach,
		nesting:          nesting,
	}, nil
}

func (v *validator) validate(ctx context.Context) *engineapi.RuleResponse {
	if err := v.loadContext(ctx); err != nil {
		return internal.RuleError(v.rule, engineapi.Validation, "failed to load context", err)
	}
	preconditionsPassed, err := internal.CheckPreconditions(v.log, v.policyContext.JSONContext(), v.anyAllConditions)
	if err != nil {
		return internal.RuleError(v.rule, engineapi.Validation, "failed to evaluate preconditions", err)
	}
	if !preconditionsPassed {
		return internal.RuleSkip(v.rule, engineapi.Validation, "preconditions not met")
	}

	if v.deny != nil {
		return v.validateDeny()
	}

	if v.pattern != nil || v.anyPattern != nil {
		if err = v.substitutePatterns(); err != nil {
			return internal.RuleError(v.rule, engineapi.Validation, "variable substitution failed", err)
		}

		ruleResponse := v.validateResourceWithRule()
		return ruleResponse
	}

	if v.forEach != nil {
		ruleResponse := v.validateForEach(ctx)
		return ruleResponse
	}

	v.log.V(2).Info("invalid validation rule: podSecurity, patterns, or deny expected")
	return nil
}

func (v *validator) validateForEach(ctx context.Context) *engineapi.RuleResponse {
	applyCount := 0
	for _, foreach := range v.forEach {
		elements, err := engineutils.EvaluateList(foreach.List, v.policyContext.JSONContext())
		if err != nil {
			v.log.V(2).Info("failed to evaluate list", "list", foreach.List, "error", err.Error())
			continue
		}
		resp, count := v.validateElements(ctx, foreach, elements, foreach.ElementScope)
		if resp.Status != engineapi.RuleStatusPass {
			return resp
		}
		applyCount += count
	}
	if applyCount == 0 {
		if v.forEach == nil {
			return nil
		}
		return internal.RuleSkip(v.rule, engineapi.Validation, "rule skipped")
	}
	return internal.RulePass(v.rule, engineapi.Validation, "rule passed")
}

func (v *validator) validateElements(ctx context.Context, foreach kyvernov1.ForEachValidation, elements []interface{}, elementScope *bool) (*engineapi.RuleResponse, int) {
	v.policyContext.JSONContext().Checkpoint()
	defer v.policyContext.JSONContext().Restore()
	applyCount := 0

	for index, element := range elements {
		if element == nil {
			continue
		}

		v.policyContext.JSONContext().Reset()
		policyContext := v.policyContext.Copy()
		if err := engineutils.AddElementToContext(policyContext, element, index, v.nesting, elementScope); err != nil {
			v.log.Error(err, "failed to add element to context")
			return internal.RuleError(v.rule, engineapi.Validation, "failed to process foreach", err), applyCount
		}

		foreachValidator, err := newForEachValidator(foreach, v.contextLoader, v.nesting+1, v.rule, policyContext, v.log)
		if err != nil {
			v.log.Error(err, "failed to create foreach validator")
			return internal.RuleError(v.rule, engineapi.Validation, "failed to create foreach validator", err), applyCount
		}

		r := foreachValidator.validate(ctx)
		if r == nil {
			v.log.V(2).Info("skip rule due to empty result")
			continue
		} else if r.Status == engineapi.RuleStatusSkip {
			v.log.V(2).Info("skip rule", "reason", r.Message)
			continue
		} else if r.Status != engineapi.RuleStatusPass {
			if r.Status == engineapi.RuleStatusError {
				if index < len(elements)-1 {
					continue
				}
				msg := fmt.Sprintf("validation failure: %v", r.Message)
				return internal.RuleResponse(v.rule, engineapi.Validation, msg, r.Status), applyCount
			}
			msg := fmt.Sprintf("validation failure: %v", r.Message)
			return internal.RuleResponse(v.rule, engineapi.Validation, msg, r.Status), applyCount
		}

		applyCount++
	}

	return internal.RulePass(v.rule, engineapi.Validation, ""), applyCount
}

func (v *validator) loadContext(ctx context.Context) error {
	if err := v.contextLoader(ctx, v.contextEntries, v.policyContext.JSONContext()); err != nil {
		if _, ok := err.(gojmespath.NotFoundError); ok {
			v.log.V(3).Info("failed to load context", "reason", err.Error())
		} else {
			v.log.Error(err, "failed to load context")
		}
		return err
	}
	return nil
}

func (v *validator) validateDeny() *engineapi.RuleResponse {
	if deny, err := internal.CheckDenyPreconditions(v.log, v.policyContext.JSONContext(), v.deny.GetAnyAllConditions()); err != nil {
		return internal.RuleError(v.rule, engineapi.Validation, "failed to check deny preconditions", err)
	} else {
		if deny {
			return internal.RuleResponse(v.rule, engineapi.Validation, v.getDenyMessage(deny), engineapi.RuleStatusFail)
		}
		return internal.RulePass(v.rule, engineapi.Validation, v.getDenyMessage(deny))
	}
}

func (v *validator) getDenyMessage(deny bool) string {
	if !deny {
		return fmt.Sprintf("validation rule '%s' passed.", v.rule.Name)
	}
	msg := v.rule.Validation.Message
	if msg == "" {
		return fmt.Sprintf("validation error: rule %s failed", v.rule.Name)
	}
	raw, err := variables.SubstituteAll(v.log, v.policyContext.JSONContext(), msg)
	if err != nil {
		return msg
	}
	switch typed := raw.(type) {
	case string:
		return typed
	default:
		return "the produced message didn't resolve to a string, check your policy definition."
	}
}

func (v *validator) validateResourceWithRule() *engineapi.RuleResponse {
	element := v.policyContext.Element()
	if !engineutils.IsEmptyUnstructured(&element) {
		return v.validatePatterns(element)
	}
	if engineutils.IsDeleteRequest(v.policyContext) {
		v.log.V(3).Info("skipping validation on deleted resource")
		return nil
	}
	resp := v.validatePatterns(v.policyContext.NewResource())
	return resp
}

// validatePatterns validate pattern and anyPattern
func (v *validator) validatePatterns(resource unstructured.Unstructured) *engineapi.RuleResponse {
	if v.pattern != nil {
		if err := validate.MatchPattern(v.log, resource.Object, v.pattern); err != nil {
			pe, ok := err.(*validate.PatternError)
			if ok {
				v.log.V(3).Info("validation error", "path", pe.Path, "error", err.Error())

				if pe.Skip {
					return internal.RuleSkip(v.rule, engineapi.Validation, pe.Error())
				}

				if pe.Path == "" {
					return internal.RuleResponse(v.rule, engineapi.Validation, v.buildErrorMessage(err, ""), engineapi.RuleStatusError)
				}

				return internal.RuleResponse(v.rule, engineapi.Validation, v.buildErrorMessage(err, pe.Path), engineapi.RuleStatusFail)
			}

			return internal.RuleResponse(v.rule, engineapi.Validation, v.buildErrorMessage(err, pe.Path), engineapi.RuleStatusError)
		}

		v.log.V(4).Info("successfully processed rule")
		msg := fmt.Sprintf("validation rule '%s' passed.", v.rule.Name)
		return internal.RulePass(v.rule, engineapi.Validation, msg)
	}

	if v.anyPattern != nil {
		var failedAnyPatternsErrors []error
		var skippedAnyPatternErrors []error
		var err error

		anyPatterns, err := deserializeAnyPattern(v.anyPattern)
		if err != nil {
			return internal.RuleError(v.rule, engineapi.Validation, "failed to deserialize anyPattern, expected type array", err)
		}

		for idx, pattern := range anyPatterns {
			err := validate.MatchPattern(v.log, resource.Object, pattern)
			if err == nil {
				msg := fmt.Sprintf("validation rule '%s' anyPattern[%d] passed.", v.rule.Name, idx)
				return internal.RulePass(v.rule, engineapi.Validation, msg)
			}

			if pe, ok := err.(*validate.PatternError); ok {
				var patternErr error
				v.log.V(3).Info("validation rule failed", "anyPattern[%d]", idx, "path", pe.Path)

				if pe.Skip {
					patternErr = fmt.Errorf("rule %s[%d] skipped: %s", v.rule.Name, idx, err.Error())
					skippedAnyPatternErrors = append(skippedAnyPatternErrors, patternErr)
				} else {
					if pe.Path == "" {
						patternErr = fmt.Errorf("rule %s[%d] failed: %s", v.rule.Name, idx, err.Error())
					} else {
						patternErr = fmt.Errorf("rule %s[%d] failed at path %s", v.rule.Name, idx, pe.Path)
					}
					failedAnyPatternsErrors = append(failedAnyPatternsErrors, patternErr)
				}
			}
		}

		// Any Pattern validation errors
		if len(skippedAnyPatternErrors) > 0 && len(failedAnyPatternsErrors) == 0 {
			var errorStr []string
			for _, err := range skippedAnyPatternErrors {
				errorStr = append(errorStr, err.Error())
			}
			v.log.V(4).Info(fmt.Sprintf("Validation rule '%s' skipped. %s", v.rule.Name, errorStr))
			return internal.RuleSkip(v.rule, engineapi.Validation, strings.Join(errorStr, " "))
		} else if len(failedAnyPatternsErrors) > 0 {
			var errorStr []string
			for _, err := range failedAnyPatternsErrors {
				errorStr = append(errorStr, err.Error())
			}

			v.log.V(4).Info(fmt.Sprintf("Validation rule '%s' failed. %s", v.rule.Name, errorStr))
			msg := buildAnyPatternErrorMessage(v.rule, errorStr)
			return internal.RuleResponse(v.rule, engineapi.Validation, msg, engineapi.RuleStatusFail)
		}
	}

	return internal.RulePass(v.rule, engineapi.Validation, v.rule.Validation.Message)
}

func deserializeAnyPattern(anyPattern apiextensions.JSON) ([]interface{}, error) {
	if anyPattern == nil {
		return nil, nil
	}

	ap, err := json.Marshal(anyPattern)
	if err != nil {
		return nil, err
	}

	var res []interface{}
	if err := json.Unmarshal(ap, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (v *validator) buildErrorMessage(err error, path string) string {
	if v.rule.Validation.Message == "" {
		if path != "" {
			return fmt.Sprintf("validation error: rule %s failed at path %s", v.rule.Name, path)
		}

		return fmt.Sprintf("validation error: rule %s execution error: %s", v.rule.Name, err.Error())
	}

	msgRaw, sErr := variables.SubstituteAll(v.log, v.policyContext.JSONContext(), v.rule.Validation.Message)
	if sErr != nil {
		v.log.V(2).Info("failed to substitute variables in message", "error", sErr)
		return fmt.Sprintf("validation error: variables substitution error in rule %s execution error: %s", v.rule.Name, err.Error())
	} else {
		msg := msgRaw.(string)
		if !strings.HasSuffix(msg, ".") {
			msg = msg + "."
		}
		if path != "" {
			return fmt.Sprintf("validation error: %s rule %s failed at path %s", msg, v.rule.Name, path)
		}
		return fmt.Sprintf("validation error: %s rule %s execution error: %s", msg, v.rule.Name, err.Error())
	}
}

func buildAnyPatternErrorMessage(rule kyvernov1.Rule, errors []string) string {
	errStr := strings.Join(errors, " ")
	if rule.Validation.Message == "" {
		return fmt.Sprintf("validation error: %s", errStr)
	}

	if strings.HasSuffix(rule.Validation.Message, ".") {
		return fmt.Sprintf("validation error: %s %s", rule.Validation.Message, errStr)
	}

	return fmt.Sprintf("validation error: %s. %s", rule.Validation.Message, errStr)
}

func (v *validator) substitutePatterns() error {
	if v.pattern != nil {
		i, err := variables.SubstituteAll(v.log, v.policyContext.JSONContext(), v.pattern)
		if err != nil {
			return err
		}
		v.pattern = i.(apiextensions.JSON)
		return nil
	}

	if v.anyPattern != nil {
		i, err := variables.SubstituteAll(v.log, v.policyContext.JSONContext(), v.anyPattern)
		if err != nil {
			return err
		}
		v.anyPattern = i.(apiextensions.JSON)
		return nil
	}

	return nil
}
