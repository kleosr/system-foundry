package main

import (
	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

const (
	demoClientID = "foundry-cli"
	demoRedirect = "http://127.0.0.1:8765/callback"
)

type demoAccount struct {
	email    string
	tenantID string
	role     string
}

func seedDemo(provider *auth.Provider, password, opsTenant string) error {
	if password == "" {
		return nil
	}
	accounts := []demoAccount{
		{email: "owner@acme.test", tenantID: "acme", role: platform.RoleOwner},
		{email: "agent@acme.test", tenantID: "acme", role: platform.RoleAgent},
		{email: "owner@beta.test", tenantID: "beta", role: platform.RoleOwner},
		{email: "operator@ops.test", tenantID: opsTenant, role: platform.RoleOwner},
	}
	for _, account := range accounts {
		if err := provider.CreateUser(account.email, password, account.tenantID, account.role); err != nil {
			return err
		}
	}
	return provider.RegisterClient(demoClientID, "foundry-api", []string{demoRedirect})
}
