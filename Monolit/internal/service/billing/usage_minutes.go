package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) CanUploadPersonalCall(ctx context.Context, userID uuid.UUID, durationSeconds int) error {
	subscription, err := s.activePersonalSubscription(ctx, userID)
	if err != nil {
		return err
	}

	return s.canUseMinutes(ctx, subscription, durationSeconds)
}

func (s *Service) CanUploadBusinessCall(ctx context.Context, companyID uuid.UUID, durationSeconds int) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	return s.canUseMinutes(ctx, subscription, durationSeconds)
}

func (s *Service) AddPersonalUsageMinutes(ctx context.Context, userID uuid.UUID, durationSeconds int) error {
	subscription, err := s.activePersonalSubscription(ctx, userID)
	if err != nil {
		return err
	}

	return s.addUsageMinutes(ctx, subscription.ID, durationSeconds)
}

func (s *Service) AddBusinessUsageMinutes(ctx context.Context, companyID uuid.UUID, durationSeconds int) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	return s.addUsageMinutes(ctx, subscription.ID, durationSeconds)
}

func (s *Service) canUseMinutes(ctx context.Context, subscription models.Subscription, durationSeconds int) error {
	minutes := minutesFromSeconds(durationSeconds)
	if minutes == 0 {
		return nil
	}

	usedMinutes, err := s.repository.CountUsedMinutes(ctx, subscription.ID, s.now())
	if err != nil {
		return err
	}

	if usedMinutes+minutes > subscription.Plan.MonthlyMinutesLimit {
		return models.ErrMonthlyMinutesLimitExceeded
	}

	return nil
}

func (s *Service) addUsageMinutes(ctx context.Context, subscriptionID uuid.UUID, durationSeconds int) error {
	minutes := minutesFromSeconds(durationSeconds)
	if minutes == 0 {
		return nil
	}

	_, err := s.repository.AddUsageMinutes(ctx, subscriptionID, s.now(), minutes)
	return err
}

func minutesFromSeconds(seconds int) int {
	if seconds <= 0 {
		return 0
	}

	return (seconds + 59) / 60
}
