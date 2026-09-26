package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/spf13/cobra"
)

var (
	errDoctorArguments = errors.New("telemetry doctor: invalid arguments")
	errDoctorCheck     = errors.New("telemetry doctor: configuration or receiver check failed")
	errDoctorOutput    = errors.New("telemetry doctor: unable to write report")
)

type doctorReport struct {
	Configuration observability.ConfigurationReport `json:"configuration"`
	Probe         *observability.ProbeReport        `json:"probe,omitempty"`
}

// NewCommandTelemetry returns commands for diagnosing telemetry setup without
// displaying endpoint, header, certificate-path, or application identity values.
func NewCommandTelemetry() *cobra.Command {
	command := &cobra.Command{Use: "telemetry", Short: "Inspect telemetry configuration and receiver acceptance"}
	var probe bool
	var timeout time.Duration
	doctor := &cobra.Command{
		Use: "doctor", Short: "Print sanitized effective telemetry configuration",
		Long: "Print sanitized effective telemetry configuration as JSON. By default this only inspects settings. " +
			"Use --probe to send one synthetic OTLP request per enabled signal under a fixed diagnostic service name. " +
			"Receiver acceptance does not establish SDK delivery or backend indexing.",
		SilenceUsage: true, SilenceErrors: true,
		Args: func(command *cobra.Command, args []string) error {
			if len(args) != 0 {
				_, _ = fmt.Fprintln(command.ErrOrStderr(), errDoctorArguments)
				return errDoctorArguments
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			if timeout <= 0 || timeout > observability.MaximumProbeTimeout {
				_, _ = fmt.Fprintln(command.ErrOrStderr(), errDoctorArguments)
				return errDoctorArguments
			}
			configuration, inspectionErr := observability.InspectConfiguration(observability.Config{})
			report := doctorReport{Configuration: configuration}
			var probeErr error
			if probe {
				result, err := observability.ProbeConfiguration(command.Context(), observability.Config{}, timeout)
				report.Probe, probeErr = &result, err
			}
			encoder := json.NewEncoder(command.OutOrStdout())
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(report); err != nil {
				return errDoctorOutput
			}
			if inspectionErr != nil || probeErr != nil {
				return errDoctorCheck
			}
			return nil
		},
	}
	doctor.Flags().BoolVar(&probe, "probe", false, "Send synthetic telemetry to check receiver acceptance")
	doctor.Flags().DurationVar(&timeout, "timeout", observability.DefaultProbeTimeout, "Total probe time budget (maximum 1m)")
	doctor.SetFlagErrorFunc(func(command *cobra.Command, _ error) error {
		_, _ = fmt.Fprintln(command.ErrOrStderr(), errDoctorArguments)
		return errDoctorArguments
	})
	command.AddCommand(doctor)
	return command
}
