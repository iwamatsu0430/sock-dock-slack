package main

import (
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
)

// OwnerState is the owner ("本人") section of Settings, or why it cannot be
// shown (the gateway is not running).
type OwnerState struct {
	control.OwnerInfo
	Error string `json:"error,omitempty"`
}

func ownerState(o control.OwnerInfo, err error) OwnerState {
	if err != nil {
		return OwnerState{Error: err.Error()}
	}
	return OwnerState{OwnerInfo: o}
}

// GetOwner returns the owner, the registration in progress and the DM
// notice settings.
func (a *AppService) GetOwner() OwnerState {
	c, cancel := ctx()
	defer cancel()
	return ownerState(a.client.Owner(c))
}

// StartOwnerRegistration shows a code to send to the Slack app by DM.
func (a *AppService) StartOwnerRegistration() OwnerState {
	c, cancel := ctx()
	defer cancel()
	return ownerState(a.client.StartRegistration(c))
}

// CancelOwnerRegistration stops accepting the code.
func (a *AppService) CancelOwnerRegistration() OwnerState {
	c, cancel := ctx()
	defer cancel()
	return ownerState(a.client.CancelRegistration(c))
}

// ClearOwner removes the owner (DM notices stop).
func (a *AppService) ClearOwner() OwnerState {
	c, cancel := ctx()
	defer cancel()
	return ownerState(a.client.ClearOwner(c))
}

// SetNotifyDM turns DM notices ("notify_dm") or DM notices of failed runs
// ("notify_dm_failures") on or off.
func (a *AppService) SetNotifyDM(key string, on bool) OwnerState {
	c, cancel := ctx()
	defer cancel()
	return ownerState(a.client.SetNotify(c, key, on))
}
