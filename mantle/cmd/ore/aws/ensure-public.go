// Copyright 2026 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package aws

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"slices"
	"sort"
	"syscall"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/platform/api/aws"
)

const (
	// AWS unshares a deprecated AMI once it hasn't been launched for six months.
	keepaliveCutoff = 183 * 24 * time.Hour
	// Buffer both deadlines: an idle AMI can be unshared as soon as it deprecates.
	keepaliveBuffer = 14 * 24 * time.Hour

	keepaliveLaunchTimeout  = 5 * time.Minute
	keepaliveCleanupTimeout = 30 * time.Second
)

var (
	cmdEnsurePublic = &cobra.Command{
		Use:   "ensure-public",
		Short: "Ensure production RHCOS AMIs remain publicly accessible",
		Long: `Keeps production RHCOS AMIs (tagged production=true) publicly accessible
despite AWS's automatic AMI deprecation policy.

AWS gives public AMIs a default deprecation date 2 years after creation and
eventually removes their public sharing permission after deprecation when no
instances have been launched for 6+ months. This breaks OpenShift customers
trying to scale cluster nodes with older images, and DisableImageDeprecation
has no effect on public AMIs.

Restoring permissions alone does not reset inactivity, so this also launches
and terminates a throwaway instance from each at-risk AMI: account-owned AMIs
that are not public, plus public ones near or past deprecation with old or
unknown launch history. A 14-day buffer applies to both thresholds. Every
at-risk AMI is restored; only the launches are capped.

Launch timestamps lag by up to 24 hours, so consecutive runs may repeat
launches. Instances are terminated on exit, including on SIGINT/SIGTERM; any
that leak carry CreatedBy=mantle for "ore aws gc" in the same region.

Exits non-zero if any AMI could not be restored or relaunched.

Examples:

  # Restore and relaunch at-risk production AMIs in a region
  ore aws ensure-public --region us-east-1

  # Report what would happen, without changing anything
  ore aws ensure-public --region us-east-1 --dry-run

  # Target a specific AMI by ID, bypassing the production tag filter
  ore aws ensure-public --region us-east-1 --ami ami-0abc123`,
		RunE:         runEnsurePublic,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}

	ensurePublicAMI         string
	ensurePublicDryRun      bool
	ensurePublicMaxLaunches int
)

func init() {
	AWS.AddCommand(cmdEnsurePublic)
	cmdEnsurePublic.Flags().StringVar(&ensurePublicAMI, "ami", "",
		"Target a single AMI by ID; bypasses production=true and age checks.")
	cmdEnsurePublic.Flags().BoolVar(&ensurePublicDryRun, "dry-run", false,
		"Report what would be restored and launched without changing anything.")
	cmdEnsurePublic.Flags().IntVar(&ensurePublicMaxLaunches, "max-launches", 25,
		"Maximum keepalive instances to launch per run, most urgent first; 0 for no limit.")
}

// target is an AMI selected for restoration, a keepalive launch, or both.
type target struct {
	id          string
	name        string
	arch        ec2types.ArchitectureValues
	deprecation string
	// lastLaunch is nil when the timestamp is absent or unparseable.
	lastLaunch  *time.Time
	needsPublic bool
}

func runEnsurePublic(cmd *cobra.Command, args []string) error {
	if ensurePublicMaxLaunches < 0 {
		return fmt.Errorf("--max-launches cannot be negative")
	}

	targets, stats, err := ensurePublicTargets()
	if err != nil {
		return err
	}
	sortByUrgency(targets)
	launching := capLaunches(targets)

	var errs []error
	// Restoring a permission is free, so restore every at-risk AMI; only the
	// launches, which cost money and quota, are capped.
	if err := restorePublic(targets); err != nil {
		errs = append(errs, err)
	}
	if err := launchKeepaliveInstances(launching); err != nil {
		errs = append(errs, err)
	}
	warnMissingLaunchHistory(stats, len(targets), len(launching))
	return errors.Join(errs...)
}

// restorePublic re-grants public launch permission on the AMIs that lost it.
func restorePublic(targets []target) error {
	failures := 0
	for _, t := range targets {
		if !t.needsPublic {
			continue
		}
		if ensurePublicDryRun {
			fmt.Printf("would restore %s (%s) — %s\n", t.id, t.name, t.describeDeprecation())
			continue
		}
		if err := API.RestoreImagePublic(t.id); err != nil {
			fmt.Fprintf(os.Stderr, "error restoring %s (%s): %v\n", t.id, t.name, err)
			failures++
			continue
		}
		fmt.Printf("restored %s (%s) — %s\n", t.id, t.name, t.describeDeprecation())
	}
	if failures > 0 {
		return fmt.Errorf("%d AMI(s) could not be made public", failures)
	}
	return nil
}

// imageStats counts what a scan saw, for checkLaunchHistory.
type imageStats struct {
	// examined is the number of images that had a usable ID.
	examined int
	// withHistory is how many of those reported a parseable LastLaunchedTime.
	withHistory int
}

// ensurePublicTargets selects AMIs needing restoration or a keepalive launch.
func ensurePublicTargets() ([]target, imageStats, error) {
	var images []ec2types.Image
	if ensurePublicAMI != "" {
		img, err := API.GetImageByID(ensurePublicAMI)
		if err != nil {
			return nil, imageStats{}, fmt.Errorf("fetching AMI %s: %v", ensurePublicAMI, err)
		}
		if img == nil {
			return nil, imageStats{}, fmt.Errorf("AMI %s not found in region %s", ensurePublicAMI, region)
		}
		images = []ec2types.Image{*img}
	} else {
		var err error
		images, err = API.ListProductionImages()
		if err != nil {
			return nil, imageStats{}, fmt.Errorf("listing production AMIs in %s: %v", region, err)
		}
	}

	targets := make([]target, 0, len(images))
	var stats imageStats
	for _, img := range images {
		t := target{
			id:          derefStr(img.ImageId),
			name:        derefStr(img.Name),
			arch:        img.Architecture,
			deprecation: derefStr(img.DeprecationTime),
			lastLaunch:  parseAWSTime(derefStr(img.LastLaunchedTime)),
		}
		if t.id == "" {
			fmt.Fprintf(os.Stderr, "skipping image with no ID\n")
			continue
		}
		stats.examined++
		if t.lastLaunch != nil {
			stats.withHistory++
		}

		// Image.Public reflects the all-group launch permission.
		t.needsPublic = img.Public == nil || !*img.Public

		// Non-public and explicit targets bypass the deprecation/inactivity checks.
		stale := t.lastLaunch == nil || time.Since(*t.lastLaunch) >= keepaliveCutoff-keepaliveBuffer
		atRisk := ensurePublicAMI != "" || t.needsPublic ||
			(isDeprecating(t.deprecation) && stale)

		if atRisk {
			targets = append(targets, t)
		}
	}

	return targets, stats, nil
}

// warnMissingLaunchHistory notes a region that reported no launch history at
// all. This is advisory and never affects the exit status: missing history is
// not evidence that this run failed, and a successful first run in a region
// legitimately reports nothing, since AWS takes up to 24 hours to report a
// launch.
//
// It describes an absent value, not a stale one. There is no local state to
// compare against, so it cannot tell whether a timestamp advanced. A nil
// LastLaunchedTime on any one AMI is ordinary -- an AMI nobody has launched
// has none, and one relaunched yesterday can still read nil today. Only a
// whole region reporting nothing is worth mentioning; the cause may be absent
// launch history, reporting delay, or unavailable data.
//
// sortByUrgency still prioritizes non-public AMIs, but cannot order AMIs by
// launch age within each group. Without updated history, capped runs may
// repeatedly select the same AMIs.
func warnMissingLaunchHistory(stats imageStats, atRisk, launching int) {
	// A single AMI proves nothing either way, and --ami only ever examines
	// one. One reported timestamp anywhere shows the field is populated.
	if stats.examined <= 1 || stats.withHistory > 0 {
		return
	}

	msg := fmt.Sprintf("no usable last-launch timestamps reported for the %d production AMIs in %s; "+
		"recent launches may not appear yet", stats.examined, region)
	if deferred := atRisk - launching; deferred > 0 {
		msg += fmt.Sprintf("; %d AMIs deferred by --max-launches; without updated history, "+
			"subsequent runs may select the same AMIs again", deferred)
	}
	fmt.Fprintf(os.Stderr, "warning: %s\n", msg)
}

// sortByUrgency puts non-public AMIs first, then unknown and oldest launches.
func sortByUrgency(targets []target) {
	sort.SliceStable(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if a.needsPublic != b.needsPublic {
			return a.needsPublic
		}
		if (a.lastLaunch == nil) != (b.lastLaunch == nil) {
			return a.lastLaunch == nil
		}
		if a.lastLaunch == nil {
			return false
		}
		return a.lastLaunch.Before(*b.lastLaunch)
	})
}

func capLaunches(targets []target) []target {
	if ensurePublicMaxLaunches <= 0 || len(targets) <= ensurePublicMaxLaunches {
		return targets
	}
	fmt.Printf("%d AMIs at risk; relaunching %d now, deferring %d to a later run\n",
		len(targets), ensurePublicMaxLaunches, len(targets)-ensurePublicMaxLaunches)
	return targets[:ensurePublicMaxLaunches]
}

// launchKeepaliveInstances launches the given AMIs, waits as a batch, and
// requests termination. Per-AMI detail goes to stderr and the returned error
// only summarizes, since the caller prints it again.
func launchKeepaliveInstances(targets []target) error {
	if len(targets) == 0 {
		return nil
	}

	var errs []error
	failures := 0

	launched := map[string]target{}
	interruptCtx, stopInterrupts := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopInterrupts()
	// Cleanup must remain usable after interruptCtx is canceled.
	cleanup := func() error {
		if len(launched) == 0 {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), keepaliveCleanupTimeout)
		defer cancel()
		if err := API.TerminateInstances(ctx, instanceIDs(launched)); err != nil {
			return err
		}
		clear(launched)
		return nil
	}
	// Cover panics; the explicit call below retries and reports errors.
	defer func() {
		if err := cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "error terminating keepalive instances %v: %v\n", instanceIDs(launched), err)
		}
	}()

	for i, t := range targets {
		// Let an in-flight launch return its ID before handling cancellation.
		if interruptCtx.Err() != nil {
			fmt.Fprintln(os.Stderr, "interrupted; stopping keepalive launches and cleaning up")
			break
		}
		if ensurePublicDryRun {
			fmt.Printf("would launch %s (%s) — last launched %s\n",
				t.id, t.name, formatLastLaunch(t.lastLaunch))
			continue
		}
		instanceID, instanceType, err := API.LaunchKeepaliveInstance(t.id, t.arch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error launching keepalive for %s (%s): %v\n", t.id, t.name, err)
			failures++
			// Stop on region-wide failures rather than repeat them for every AMI.
			if errors.Is(err, aws.ErrInstanceQuotaExceeded) || errors.Is(err, aws.ErrNoDefaultVPC) {
				fmt.Fprintf(os.Stderr, "skipping the remaining %d AMI(s) in %s\n",
					len(targets)-i-1, region)
				break
			}
			continue
		}
		launched[instanceID] = t
		fmt.Printf("launched %s from %s (%s) %s — last launched %s\n",
			instanceID, t.id, t.name, instanceType, formatLastLaunch(t.lastLaunch))
	}

	if ids := instanceIDs(launched); len(ids) > 0 {
		// Check EC2 startup before cleanup; "running" doesn't prove guest health.
		if interruptCtx.Err() == nil {
			startFailures, err := API.WaitForInstancesRunning(interruptCtx, ids, keepaliveLaunchTimeout)
			// Cancellation is reported once, below.
			if err != nil && interruptCtx.Err() == nil {
				errs = append(errs, err)
			}
			// Keep failure logs in stable instance-ID order.
			for _, id := range ids {
				failure, ok := startFailures[id]
				if !ok {
					continue
				}
				t := launched[id]
				fmt.Fprintf(os.Stderr, "keepalive %s for %s (%s) failed to start: %v\n", id, t.id, t.name, failure)
				failures++
			}
		}
		// Retry once before reporting: a leak here costs money until the next
		// "ore aws gc", but a retracted failure costs a false alarm every day.
		err := cleanup()
		if err != nil {
			err = cleanup()
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("could not terminate keepalive instances %v: %v", ids, err))
		} else {
			fmt.Printf("requested termination of keepalive instances %v\n", ids)
		}
	}

	if failures > 0 {
		errs = append(errs, fmt.Errorf("%d AMI(s) could not be relaunched", failures))
	}
	if interruptCtx.Err() != nil {
		errs = append(errs, errors.New("interrupted; some at-risk AMIs may not have been relaunched"))
	}

	return errors.Join(errs...)
}

func instanceIDs(launched map[string]target) []string {
	return slices.Sorted(maps.Keys(launched))
}

// isDeprecating reports whether an AMI deprecates within keepaliveBuffer, or has.
func isDeprecating(deprecation string) bool {
	t := parseAWSTime(deprecation)
	return t != nil && t.Before(time.Now().Add(keepaliveBuffer))
}

// parseAWSTime parses an EC2 timestamp, returning nil if absent or unparseable.
// RFC3339 parsing also accepts fractional seconds.
func parseAWSTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func (t target) describeDeprecation() string {
	if t.deprecation == "" {
		return "no deprecation date"
	}
	date := t.deprecation
	if parsed := parseAWSTime(date); parsed != nil {
		date = parsed.Format("2006-01-02")
	}
	return fmt.Sprintf("deprecation date %s", date)
}

func formatLastLaunch(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.Format("2006-01-02")
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
