package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// SSM documents created next to the host (terraform_practics,
// infra/aws/ec2/cloudpanel_ssm.tf). CloudPanel has no API, the documents wrap clpctl.
const (
	docList      = "cloudpanel-site-list"
	docAddStatic = "cloudpanel-site-add-static"
	docDelete    = "cloudpanel-site-delete"

	// GetCommandInvocation truncates StandardOutputContent at 24000 characters.
	maxInlineOutput = 24000
)

type ssmAPI interface {
	SendCommand(ctx context.Context, in *ssm.SendCommandInput, opts ...func(*ssm.Options)) (*ssm.SendCommandOutput, error)
	GetCommandInvocation(ctx context.Context, in *ssm.GetCommandInvocationInput, opts ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error)
}

type site struct {
	DomainName    string `json:"domain_name"`
	Type          string `json:"type"`
	User          string `json:"user"`
	RootDirectory string `json:"root_directory"`
}

type client struct {
	ssm          ssmAPI
	instanceID   string
	pollInterval time.Duration
	timeout      time.Duration

	// One list command per provider run instead of one per site on refresh.
	mu    sync.Mutex
	sites map[string]site
}

func (c *client) run(ctx context.Context, document string, params map[string][]string) (string, error) {
	out, err := c.ssm.SendCommand(ctx, &ssm.SendCommandInput{
		DocumentName: aws.String(document),
		InstanceIds:  []string{c.instanceID},
		Parameters:   params,
	})
	if err != nil {
		return "", fmt.Errorf("send %s: %w", document, err)
	}
	commandID := aws.ToString(out.Command.CommandId)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("%s (command %s): %w", document, commandID, ctx.Err())
		case <-time.After(c.pollInterval):
		}
		inv, err := c.ssm.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
			CommandId:  aws.String(commandID),
			InstanceId: aws.String(c.instanceID),
		})
		var notYet *ssmtypes.InvocationDoesNotExist
		if errors.As(err, &notYet) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%s (command %s): %w", document, commandID, err)
		}
		switch inv.Status {
		case ssmtypes.CommandInvocationStatusSuccess:
			return aws.ToString(inv.StandardOutputContent), nil
		case ssmtypes.CommandInvocationStatusPending, ssmtypes.CommandInvocationStatusInProgress, ssmtypes.CommandInvocationStatusDelayed:
			continue
		default:
			return "", fmt.Errorf("%s (command %s) finished with %s: %s%s", document, commandID, inv.Status,
				aws.ToString(inv.StandardErrorContent), aws.ToString(inv.StandardOutputContent))
		}
	}
}

func (c *client) listSites(ctx context.Context) (map[string]site, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sites != nil {
		return c.sites, nil
	}
	out, err := c.run(ctx, docList, nil)
	if err != nil {
		return nil, err
	}
	if len(out) >= maxInlineOutput {
		return nil, fmt.Errorf("%s output reached the SSM inline limit (%d chars), the site list may be truncated", docList, maxInlineOutput)
	}
	var list []site
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("parse %s output: %w", docList, err)
	}
	sites := make(map[string]site, len(list))
	for _, s := range list {
		sites[s.DomainName] = s
	}
	c.sites = sites
	return sites, nil
}

func (c *client) invalidate() {
	c.mu.Lock()
	c.sites = nil
	c.mu.Unlock()
}

func (c *client) addStaticSite(ctx context.Context, domainName, siteUser string) error {
	defer c.invalidate()
	_, err := c.run(ctx, docAddStatic, map[string][]string{
		"domainName": {domainName},
		"siteUser":   {siteUser},
	})
	return err
}

func (c *client) deleteSite(ctx context.Context, domainName string) error {
	defer c.invalidate()
	_, err := c.run(ctx, docDelete, map[string][]string{"domainName": {domainName}})
	return err
}
