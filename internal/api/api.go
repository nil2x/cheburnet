package api

import (
	"errors"
	"fmt"

	"github.com/nil2x/cheburnet/internal/config"
)

// ValidateClub checks that the given club configuration is valid for usage with VKClient.
func ValidateClub(vkC *VKClient, club config.Club) error {
	perm, err := vkC.GroupsGetTokenPermissions(club)

	if err != nil {
		return err
	}

	bits := []int{0, 2, 12, 13, 17, 18, 27}

	for _, bit := range bits {
		if perm.Mask&(1<<bit) == 0 {
			return fmt.Errorf("permission bit %v is disabled", bit)
		}
	}

	return nil
}

// ValidateUser checks that the given user configuration is valid for usage with VKClient.
func ValidateUser(vkC *VKClient, user config.User) error {
	perm, err := vkC.AccountGetAppPermissions(user)

	if err != nil {
		return err
	}

	bits := []int{2, 4, 16, 18, 27}

	for _, bit := range bits {
		if perm.Mask&(1<<bit) == 0 {
			return fmt.Errorf("permission bit %v is disabled", bit)
		}
	}

	return nil
}

// ValidateLongPoll checks that Long Poll is configured correctly for usage with VKClient.
func ValidateLongPoll(vkC *VKClient, club config.Club) error {
	settings, err := vkC.GroupsGetLongPollSettings(club)

	if err != nil {
		return err
	}

	if !settings.IsEnabled {
		return errors.New("long poll is disabled")
	}

	for _, event := range supportedUpdateType {
		enabled, exists := settings.Events[event]

		if !exists || enabled == 0 {
			return fmt.Errorf("event %v is disabled", event)
		}
	}

	return nil
}
