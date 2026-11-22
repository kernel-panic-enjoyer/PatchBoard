package updater

import (
	"context"
	"strings"
	"time"
)

const (
	rejectedWingetOfferDiagnostic = "Skipped: WinGet reports that this package update is no longer actionable"
	rejectedWingetOfferLifetime   = 24 * time.Hour
)

type RejectedWingetOffer struct {
	InstalledVersion string `json:"installed_version"`
	AvailableVersion string `json:"available_version"`
	RejectedAt       string `json:"rejected_at"`
}

func rejectedWingetOfferKey(pkg Package) string {
	if pkg.Manager != managerWinget || strings.TrimSpace(pkg.ID) == "" {
		return ""
	}
	return packageKey(managerWinget, strings.ToLower(strings.TrimSpace(pkg.ID)))
}

func rejectedWingetOfferCurrent(offer RejectedWingetOffer, now time.Time) bool {
	rejectedAt, err := time.Parse(time.RFC3339, offer.RejectedAt)
	return err == nil && !rejectedAt.After(now) && now.Sub(rejectedAt) < rejectedWingetOfferLifetime
}

func suppressRejectedWingetOffer(state State, pkg *Package) {
	key := rejectedWingetOfferKey(*pkg)
	if key == "" || !pkg.UpdateAvailable {
		return
	}
	offer, ok := state.RejectedWingetOffers[key]
	if !ok || !rejectedWingetOfferCurrent(offer, time.Now().UTC()) {
		return
	}
	if offer.InstalledVersion == pkg.Version && offer.AvailableVersion == pkg.AvailableVersion {
		pkg.UpdateAvailable = false
		pkg.AvailableVersion = ""
	}
}

func isRejectedWingetOfferResult(result CommandResult) bool {
	return result.Code == commandSkippedCode && strings.Contains(result.Stdout, rejectedWingetOfferDiagnostic)
}

func recordRejectedWingetOffers(ctx context.Context, packages []Package, results []UpdateResult) {
	hasRejectedOffer := false
	for _, result := range results {
		if isRejectedWingetOfferResult(result.Result) {
			hasRejectedOffer = true
			break
		}
	}
	if !hasRejectedOffer {
		return
	}
	store, err := defaultStateStore()
	if err != nil {
		appLogContext(ctx, "Could not open state to remember rejected WinGet offers: %s", err)
		return
	}
	if err := recordRejectedWingetOffersWithStore(ctx, store, packages, results); err != nil {
		appLogContext(ctx, "Could not remember rejected WinGet offers: %s", err)
	}
}

func recordRejectedWingetOffersWithStore(ctx context.Context, store StateStore, packages []Package, results []UpdateResult) error {
	packagesByKey := make(map[string]Package, len(packages))
	for _, pkg := range packages {
		if key := rejectedWingetOfferKey(pkg); key != "" {
			packagesByKey[key] = pkg
		}
	}
	rejected := map[string]RejectedWingetOffer{}
	for _, result := range results {
		if !isRejectedWingetOfferResult(result.Result) {
			continue
		}
		key := strings.ToLower(result.Key)
		pkg, ok := packagesByKey[key]
		if !ok || strings.TrimSpace(pkg.AvailableVersion) == "" {
			continue
		}
		rejected[key] = RejectedWingetOffer{
			InstalledVersion: pkg.Version,
			AvailableVersion: pkg.AvailableVersion,
			RejectedAt:       utcNow(),
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	_, err := store.Update(ctx, func(state *State) error {
		if state.RejectedWingetOffers == nil {
			state.RejectedWingetOffers = map[string]RejectedWingetOffer{}
		}
		for key, offer := range rejected {
			state.RejectedWingetOffers[key] = offer
		}
		return nil
	})
	return err
}

func trimRejectedWingetOffers(entries map[string]RejectedWingetOffer) map[string]RejectedWingetOffer {
	trimmed := make(map[string]RejectedWingetOffer)
	now := time.Now().UTC()
	for _, key := range sortedMapKeys(entries) {
		if len(trimmed) >= maxStateRejectedWingetOffers {
			break
		}
		offer := entries[key]
		if !strings.HasPrefix(key, managerWinget+":") || !rejectedWingetOfferCurrent(offer, now) {
			continue
		}
		trimmed[truncateUTF8String(key, maxStateStringBytes)] = RejectedWingetOffer{
			InstalledVersion: truncateUTF8String(offer.InstalledVersion, maxStateStringBytes),
			AvailableVersion: truncateUTF8String(offer.AvailableVersion, maxStateStringBytes),
			RejectedAt:       offer.RejectedAt,
		}
	}
	return trimmed
}
