// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmdline

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

func init() {
	os.Setenv("CMDLINE_WIDTH", "80") // make sure the formatting stays the same.
}

var (
	errEchoStr        = "echo error"
	flagExtra         bool
	optNoNewline      bool
	flagTopLevelExtra bool

	errUsageStr = fmt.Sprint(ErrUsage)
)

// runEcho is used to implement commands for our tests.
func runEcho(env *Env, args []string) error {
	if len(args) == 1 {
		if args[0] == "error" {
			return errors.New(errEchoStr)
		} else if args[0] == "bad_arg" {
			return env.UsageErrorf("Invalid argument %v", args[0])
		}
	}
	if flagExtra {
		args = append(args, "extra")
	}
	if flagTopLevelExtra {
		args = append(args, "tlextra")
	}
	if optNoNewline {
		fmt.Fprint(env.Stdout, args)
	} else {
		fmt.Fprintln(env.Stdout, args)
	}
	return nil
}

// runHello is another function for test commands.
func runHello(env *Env, args []string) error {
	if flagTopLevelExtra {
		args = append(args, "tlextra")
	}
	fmt.Fprintln(env.Stdout, strings.Join(append([]string{"Hello"}, args...), " "))
	return nil
}

type testCase struct {
	Args        []string
	Envs        map[string]string
	Err         string
	Stdout      string
	Stderr      string
	GlobalFlag1 string
	GlobalFlag2 int64
}

func stripTestFlags(got string) string {
	// The global flags include the flags from the testing package, so strip them
	// out before the comparison.
	re := regexp.MustCompile(" -test[^\n]+\n(?:   [^\n]+\n)+")
	return re.ReplaceAllLiteralString(got, "")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}

func runTestCases(t *testing.T, cmd *Command, tests []testCase) {
	for _, test := range tests {
		// Reset global variables before running each test case.
		var stdout, stderr bytes.Buffer
		flagExtra, flagTopLevelExtra, optNoNewline = false, false, false
		origEnvs := make(map[string]string)

		// Set a fresh flag.CommandLine and fresh envvars for each run.
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
		for k, v := range test.Envs {
			origEnvs[k] = os.Getenv(k)
			if err := os.Setenv(k, v); err != nil {
				t.Fatalf("os.Setenv(%v, %v) failed: %v", k, v, err)
			}
		}

		var globalFlag1 string
		flag.StringVar(&globalFlag1, "global1", "", "global test flag 1")
		globalFlag2 := flag.Int64("global2", 0, "global test flag 2")
		if flag.CommandLine.Parsed() {
			t.Errorf("flag.CommandLine should not be parsed yet")
		}

		// Parse and run the command and check against expected results.
		parseOK := false
		env := &Env{Stdout: &stdout, Stderr: &stderr}
		runner, args, err := Parse(cmd, env, test.Args)
		if err == nil {
			err = runner.Run(env, args)
			parseOK = true
		}
		if got, want := errString(err), test.Err; got != want {
			t.Errorf("Ran with args %q envs %q\n GOT error:\n%q\nWANT error:\n%q", test.Args, test.Envs, got, want)
		}
		if got, want := stripTestFlags(stdout.String()), test.Stdout; got != want {
			t.Errorf("Ran with args %q envs %q\n GOT stdout:\n%q\nWANT stdout:\n%q", test.Args, test.Envs, got, want)
		}
		if got, want := stripTestFlags(stderr.String()), test.Stderr; got != want {
			t.Errorf("Ran with args %q envs %q\n GOT stderr:\n%q\nWANT stderr:\n%q", test.Args, test.Envs, got, want)
		}
		if got, want := globalFlag1, test.GlobalFlag1; got != want {
			t.Errorf("global1 flag got %q, want %q", got, want)
		}
		if got, want := *globalFlag2, test.GlobalFlag2; got != want {
			t.Errorf("global2 flag got %q, want %q", got, want)
		}

		if parseOK && !flag.CommandLine.Parsed() {
			t.Errorf("flag.CommandLine should be parsed by now")
		}
		for k, v := range origEnvs {
			if err := os.Setenv(k, v); err != nil {
				t.Fatalf("os.Setenv(%v, %v) failed: %v", k, v, err)
			}
		}
	}
}

func TestNoChildrenOrRunner(t *testing.T) {
	neither := &Command{
		Name:  "neither",
		Short: "Neither is invalid.",
		Long:  "Neither has no commands and no runner.",
	}
	wantErr := `neither: CODE INVARIANT BROKEN; FIX YOUR CODE

At least one of Children or Runner must be specified.
`
	tests := []testCase{
		{Args: []string{}, Err: wantErr},
		{Args: []string{"foo"}, Err: wantErr},
	}
	runTestCases(t, neither, tests)
	parent := &Command{
		Name:     "parent",
		Short:    "parent",
		Long:     "parent",
		Children: []*Command{neither},
	}
	wantErr = "parent " + wantErr
	tests = []testCase{
		{Args: []string{}, Err: wantErr},
		{Args: []string{"foo"}, Err: wantErr},
	}
	runTestCases(t, parent, tests)
}

func TestBothChildrenAndRunnerWithArgs(t *testing.T) {
	child := &Command{
		Name:   "child",
		Short:  "Child command.",
		Long:   "Child command.",
		Runner: RunnerFunc(runEcho),
	}
	both := &Command{
		Name:     "both",
		Short:    "Both is invalid.",
		Long:     "Both has both commands and a runner with args.",
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be echoed.",
		Children: []*Command{child},
		Runner:   RunnerFunc(runEcho),
	}
	wantErr := `both: CODE INVARIANT BROKEN; FIX YOUR CODE

Since both Children and Runner are specified, the Runner cannot take args.
Otherwise a conflict between child names and runner args is possible.
`
	tests := []testCase{
		{Args: []string{}, Err: wantErr},
		{Args: []string{"foo"}, Err: wantErr},
	}
	runTestCases(t, both, tests)
	parent := &Command{
		Name:     "parent",
		Short:    "parent",
		Long:     "parent",
		Children: []*Command{both},
	}
	wantErr = "parent " + wantErr
	tests = []testCase{
		{Args: []string{}, Err: wantErr},
		{Args: []string{"foo"}, Err: wantErr},
	}
	runTestCases(t, parent, tests)
}

func TestBothChildrenAndRunnerNoArgs(t *testing.T) {
	cmdEcho := &Command{
		Name:     "echo",
		Short:    "Print strings on stdout",
		Long:     "Echo prints any strings passed in to stdout.",
		Runner:   RunnerFunc(runEcho),
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be echoed.",
	}
	prog := &Command{
		Name:     "cmdrun",
		Short:    "Cmdrun program.",
		Long:     "Cmdrun has the echo command and a Run function with no args.",
		Children: []*Command{cmdEcho},
		Runner:   RunnerFunc(runHello),
	}
	var tests = []testCase{
		{
			Args:   []string{},
			Stdout: "Hello\n",
		},
		{
			Args: []string{"foo"},
			Err:  errUsageStr,
			Stderr: `ERROR: cmdrun: unknown command "foo"

Cmdrun has the echo command and a Run function with no args.

Usage:
   cmdrun
   cmdrun <command>

The cmdrun commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "cmdrun help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help"},
			Stdout: `Cmdrun has the echo command and a Run function with no args.

Usage:
   cmdrun
   cmdrun <command>

The cmdrun commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "cmdrun help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "echo"},
			Stdout: `Echo prints any strings passed in to stdout.

Usage:
   cmdrun echo [strings]

[strings] are arbitrary strings that will be echoed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "..."},
			Stdout: `Cmdrun has the echo command and a Run function with no args.

Usage:
   cmdrun
   cmdrun <command>

The cmdrun commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "cmdrun help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Cmdrun echo

Echo prints any strings passed in to stdout.

Usage:
   cmdrun echo [strings]

[strings] are arbitrary strings that will be echoed.
================================================================================
Cmdrun help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   cmdrun help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The cmdrun help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
		{
			Args: []string{"help", "foo"},
			Err:  errUsageStr,
			Stderr: `ERROR: cmdrun: unknown command or topic "foo"

Cmdrun has the echo command and a Run function with no args.

Usage:
   cmdrun
   cmdrun <command>

The cmdrun commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "cmdrun help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args:   []string{"echo", "foo", "bar"},
			Stdout: "[foo bar]\n",
		},
		{
			Args: []string{"echo", "error"},
			Err:  errEchoStr,
		},
		{
			Args: []string{"echo", "bad_arg"},
			Err:  errUsageStr,
			Stderr: `ERROR: Invalid argument bad_arg

Echo prints any strings passed in to stdout.

Usage:
   cmdrun echo [strings]

[strings] are arbitrary strings that will be echoed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
	}
	runTestCases(t, prog, tests)
}

func TestOneCommand(t *testing.T) {
	cmdEcho := &Command{
		Name:  "echo",
		Short: "Print strings on stdout",
		Long: `
Echo prints any strings passed in to stdout.
`,
		Runner:   RunnerFunc(runEcho),
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be echoed.",
	}
	prog := &Command{
		Name:     "onecmd",
		Short:    "Onecmd program.",
		Long:     "Onecmd only has the echo command.",
		Children: []*Command{cmdEcho},
	}

	tests := []testCase{
		{
			Args: []string{},
			Err:  errUsageStr,
			Stderr: `ERROR: onecmd: no command specified

Onecmd only has the echo command.

Usage:
   onecmd <command>

The onecmd commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "onecmd help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"foo"},
			Err:  errUsageStr,
			Stderr: `ERROR: onecmd: unknown command "foo"

Onecmd only has the echo command.

Usage:
   onecmd <command>

The onecmd commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "onecmd help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help"},
			Stdout: `Onecmd only has the echo command.

Usage:
   onecmd <command>

The onecmd commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "onecmd help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "echo"},
			Stdout: `Echo prints any strings passed in to stdout.

Usage:
   onecmd echo [strings]

[strings] are arbitrary strings that will be echoed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "help"},
			Stdout: `Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   onecmd help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The onecmd help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "..."},
			Stdout: `Onecmd only has the echo command.

Usage:
   onecmd <command>

The onecmd commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "onecmd help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Onecmd echo

Echo prints any strings passed in to stdout.

Usage:
   onecmd echo [strings]

[strings] are arbitrary strings that will be echoed.
================================================================================
Onecmd help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   onecmd help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The onecmd help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
		{
			Args: []string{"help", "foo"},
			Err:  errUsageStr,
			Stderr: `ERROR: onecmd: unknown command or topic "foo"

Onecmd only has the echo command.

Usage:
   onecmd <command>

The onecmd commands are:
   echo        Print strings on stdout
   help        Display help for commands or topics
Run "onecmd help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args:   []string{"echo", "foo", "bar"},
			Stdout: "[foo bar]\n",
		},
		{
			Args: []string{"echo", "error"},
			Err:  errEchoStr,
		},
		{
			Args: []string{"echo", "bad_arg"},
			Err:  errUsageStr,
			Stderr: `ERROR: Invalid argument bad_arg

Echo prints any strings passed in to stdout.

Usage:
   onecmd echo [strings]

[strings] are arbitrary strings that will be echoed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
	}
	runTestCases(t, prog, tests)
}

func TestMultiCommands(t *testing.T) {
	cmdEcho := &Command{
		Runner: RunnerFunc(runEcho),
		Name:   "echo",
		Short:  "Print strings on stdout",
		Long: `
Echo prints any strings passed in to stdout.
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be echoed.",
	}
	var cmdEchoOpt = &Command{
		Runner: RunnerFunc(runEcho),
		Name:   "echoopt",
		Short:  "Print strings on stdout, with opts",
		// Try varying number of header/trailer newlines around the long description.
		Long: `Echoopt prints any args passed in to stdout.


`,
		ArgsName: "[args]",
		ArgsLong: "[args] are arbitrary strings that will be echoed.",
	}
	cmdEchoOpt.Flags.BoolVar(&optNoNewline, "n", false, "Do not output trailing newline")

	prog := &Command{
		Name:     "multi",
		Short:    "Multi test command",
		Long:     "Multi has two variants of echo.",
		Children: []*Command{cmdEcho, cmdEchoOpt},
	}
	prog.Flags.BoolVar(&flagExtra, "extra", false, "Print an extra arg")

	var tests = []testCase{
		{
			Args: []string{},
			Err:  errUsageStr,
			Stderr: `ERROR: multi: no command specified

Multi has two variants of echo.

Usage:
   multi [flags] <command>

The multi commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts
   help        Display help for commands or topics
Run "multi help [command]" for command usage.

The multi flags are:
 -extra=false
   Print an extra arg

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help"},
			Stdout: `Multi has two variants of echo.

Usage:
   multi [flags] <command>

The multi commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts
   help        Display help for commands or topics
Run "multi help [command]" for command usage.

The multi flags are:
 -extra=false
   Print an extra arg

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "..."},
			Stdout: `Multi has two variants of echo.

Usage:
   multi [flags] <command>

The multi commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts
   help        Display help for commands or topics
Run "multi help [command]" for command usage.

The multi flags are:
 -extra=false
   Print an extra arg

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Multi echo

Echo prints any strings passed in to stdout.

Usage:
   multi echo [strings]

[strings] are arbitrary strings that will be echoed.
================================================================================
Multi echoopt

Echoopt prints any args passed in to stdout.

Usage:
   multi echoopt [flags] [args]

[args] are arbitrary strings that will be echoed.

The multi echoopt flags are:
 -n=false
   Do not output trailing newline
================================================================================
Multi help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   multi help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The multi help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
		{
			Args: []string{"help", "echo"},
			Stdout: `Echo prints any strings passed in to stdout.

Usage:
   multi echo [strings]

[strings] are arbitrary strings that will be echoed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "echoopt"},
			Stdout: `Echoopt prints any args passed in to stdout.

Usage:
   multi echoopt [flags] [args]

[args] are arbitrary strings that will be echoed.

The multi echoopt flags are:
 -n=false
   Do not output trailing newline

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "foo"},
			Err:  errUsageStr,
			Stderr: `ERROR: multi: unknown command or topic "foo"

Multi has two variants of echo.

Usage:
   multi [flags] <command>

The multi commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts
   help        Display help for commands or topics
Run "multi help [command]" for command usage.

The multi flags are:
 -extra=false
   Print an extra arg

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args:   []string{"echo", "foo", "bar"},
			Stdout: "[foo bar]\n",
		},
		{
			Args:   []string{"-extra", "echo", "foo", "bar"},
			Stdout: "[foo bar extra]\n",
		},
		{
			Args: []string{"echo", "error"},
			Err:  errEchoStr,
		},
		{
			Args:   []string{"echoopt", "foo", "bar"},
			Stdout: "[foo bar]\n",
		},
		{
			Args:   []string{"-extra", "echoopt", "foo", "bar"},
			Stdout: "[foo bar extra]\n",
		},
		{
			Args:   []string{"echoopt", "-n", "foo", "bar"},
			Stdout: "[foo bar]",
		},
		{
			Args:   []string{"-extra", "echoopt", "-n", "foo", "bar"},
			Stdout: "[foo bar extra]",
		},
		{
			Args:        []string{"-global1=globalStringValue", "-extra", "echoopt", "-n", "foo", "bar"},
			Stdout:      "[foo bar extra]",
			GlobalFlag1: "globalStringValue",
		},
		{
			Args:        []string{"-global2=42", "echoopt", "-n", "foo", "bar"},
			Stdout:      "[foo bar]",
			GlobalFlag2: 42,
		},
		{
			Args:        []string{"-global1=globalStringOtherValue", "-global2=43", "-extra", "echoopt", "-n", "foo", "bar"},
			Stdout:      "[foo bar extra]",
			GlobalFlag1: "globalStringOtherValue",
			GlobalFlag2: 43,
		},
		{
			Args: []string{"echoopt", "error"},
			Err:  errEchoStr,
		},
		{
			Args: []string{"echo", "-n", "foo", "bar"},
			Err:  errUsageStr,
			Stderr: `ERROR: multi echo: flag provided but not defined: -n

Echo prints any strings passed in to stdout.

Usage:
   multi echo [strings]

[strings] are arbitrary strings that will be echoed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"-nosuchflag", "echo", "foo", "bar"},
			Err:  errUsageStr,
			Stderr: `ERROR: multi: flag provided but not defined: -nosuchflag

Multi has two variants of echo.

Usage:
   multi [flags] <command>

The multi commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts
   help        Display help for commands or topics
Run "multi help [command]" for command usage.

The multi flags are:
 -extra=false
   Print an extra arg

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
	}
	runTestCases(t, prog, tests)
}

func TestMultiLevelCommands(t *testing.T) {
	cmdEcho := &Command{
		Runner: RunnerFunc(runEcho),
		Name:   "echo",
		Short:  "Print strings on stdout",
		Long: `
Echo prints any strings passed in to stdout.
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be echoed.",
	}
	cmdEchoOpt := &Command{
		Runner: RunnerFunc(runEcho),
		Name:   "echoopt",
		Short:  "Print strings on stdout, with opts",
		// Try varying number of header/trailer newlines around the long description.
		Long: `Echoopt prints any args passed in to stdout.


`,
		ArgsName: "[args]",
		ArgsLong: "[args] are arbitrary strings that will be echoed.",
	}
	cmdEchoOpt.Flags.BoolVar(&optNoNewline, "n", false, "Do not output trailing newline")
	cmdHello := &Command{
		Runner: RunnerFunc(runHello),
		Name:   "hello",
		Short:  "Print strings on stdout preceded by \"Hello\"",
		Long: `
Hello prints any strings passed in to stdout preceded by "Hello".
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be printed.",
	}
	echoProg := &Command{
		Name:     "echoprog",
		Short:    "Set of echo commands",
		Long:     "Echoprog has two variants of echo.",
		Children: []*Command{cmdEcho, cmdEchoOpt},
		Topics: []Topic{
			{Name: "topic3", Short: "Help topic 3 short", Long: "Help topic 3 long."},
		},
	}
	echoProg.Flags.BoolVar(&flagExtra, "extra", false, "Print an extra arg")
	prog := &Command{
		Name:     "toplevelprog",
		Short:    "Top level prog",
		Long:     "Toplevelprog has the echo subprogram and the hello command.",
		Children: []*Command{echoProg, cmdHello},
		Topics: []Topic{
			{Name: "topic1", Short: "Help topic 1 short", Long: "Help topic 1 long."},
			{Name: "topic2", Short: "Help topic 2 short", Long: "Help topic 2 long."},
		},
	}
	prog.Flags.BoolVar(&flagTopLevelExtra, "tlextra", false, "Print an extra arg for all commands")

	var tests = []testCase{
		{
			Args: []string{},
			Err:  errUsageStr,
			Stderr: `ERROR: toplevelprog: no command specified

Toplevelprog has the echo subprogram and the hello command.

Usage:
   toplevelprog [flags] <command>

The toplevelprog commands are:
   echoprog    Set of echo commands
   hello       Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "toplevelprog help [command]" for command usage.

The toplevelprog additional help topics are:
   topic1      Help topic 1 short
   topic2      Help topic 2 short
Run "toplevelprog help [topic]" for topic details.

The toplevelprog flags are:
 -tlextra=false
   Print an extra arg for all commands

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help"},
			Stdout: `Toplevelprog has the echo subprogram and the hello command.

Usage:
   toplevelprog [flags] <command>

The toplevelprog commands are:
   echoprog    Set of echo commands
   hello       Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "toplevelprog help [command]" for command usage.

The toplevelprog additional help topics are:
   topic1      Help topic 1 short
   topic2      Help topic 2 short
Run "toplevelprog help [topic]" for topic details.

The toplevelprog flags are:
 -tlextra=false
   Print an extra arg for all commands

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "..."},
			Stdout: `Toplevelprog has the echo subprogram and the hello command.

Usage:
   toplevelprog [flags] <command>

The toplevelprog commands are:
   echoprog    Set of echo commands
   hello       Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "toplevelprog help [command]" for command usage.

The toplevelprog additional help topics are:
   topic1      Help topic 1 short
   topic2      Help topic 2 short
Run "toplevelprog help [topic]" for topic details.

The toplevelprog flags are:
 -tlextra=false
   Print an extra arg for all commands

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Toplevelprog echoprog

Echoprog has two variants of echo.

Usage:
   toplevelprog echoprog [flags] <command>

The toplevelprog echoprog commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts

The toplevelprog echoprog additional help topics are:
   topic3      Help topic 3 short

The toplevelprog echoprog flags are:
 -extra=false
   Print an extra arg
================================================================================
Toplevelprog echoprog echo

Echo prints any strings passed in to stdout.

Usage:
   toplevelprog echoprog echo [strings]

[strings] are arbitrary strings that will be echoed.
================================================================================
Toplevelprog echoprog echoopt

Echoopt prints any args passed in to stdout.

Usage:
   toplevelprog echoprog echoopt [flags] [args]

[args] are arbitrary strings that will be echoed.

The toplevelprog echoprog echoopt flags are:
 -n=false
   Do not output trailing newline
================================================================================
Toplevelprog echoprog topic3 - help topic

Help topic 3 long.
================================================================================
Toplevelprog hello

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   toplevelprog hello [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Toplevelprog help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   toplevelprog help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The toplevelprog help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
================================================================================
Toplevelprog topic1 - help topic

Help topic 1 long.
================================================================================
Toplevelprog topic2 - help topic

Help topic 2 long.
`,
		},
		{
			Args: []string{"help", "echoprog"},
			Stdout: `Echoprog has two variants of echo.

Usage:
   toplevelprog echoprog [flags] <command>

The toplevelprog echoprog commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts
   help        Display help for commands or topics
Run "toplevelprog echoprog help [command]" for command usage.

The toplevelprog echoprog additional help topics are:
   topic3      Help topic 3 short
Run "toplevelprog echoprog help [topic]" for topic details.

The toplevelprog echoprog flags are:
 -extra=false
   Print an extra arg

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "topic1"},
			Stdout: `Help topic 1 long.
`,
		},
		{
			Args: []string{"help", "topic2"},
			Stdout: `Help topic 2 long.
`,
		},
		{
			Args: []string{"echoprog", "help", "..."},
			Stdout: `Echoprog has two variants of echo.

Usage:
   toplevelprog echoprog [flags] <command>

The toplevelprog echoprog commands are:
   echo        Print strings on stdout
   echoopt     Print strings on stdout, with opts
   help        Display help for commands or topics
Run "toplevelprog echoprog help [command]" for command usage.

The toplevelprog echoprog additional help topics are:
   topic3      Help topic 3 short
Run "toplevelprog echoprog help [topic]" for topic details.

The toplevelprog echoprog flags are:
 -extra=false
   Print an extra arg

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Toplevelprog echoprog echo

Echo prints any strings passed in to stdout.

Usage:
   toplevelprog echoprog echo [strings]

[strings] are arbitrary strings that will be echoed.
================================================================================
Toplevelprog echoprog echoopt

Echoopt prints any args passed in to stdout.

Usage:
   toplevelprog echoprog echoopt [flags] [args]

[args] are arbitrary strings that will be echoed.

The toplevelprog echoprog echoopt flags are:
 -n=false
   Do not output trailing newline
================================================================================
Toplevelprog echoprog help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   toplevelprog echoprog help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The toplevelprog echoprog help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
================================================================================
Toplevelprog echoprog topic3 - help topic

Help topic 3 long.
`,
		},
		{
			Args: []string{"echoprog", "help", "echoopt"},
			Stdout: `Echoopt prints any args passed in to stdout.

Usage:
   toplevelprog echoprog echoopt [flags] [args]

[args] are arbitrary strings that will be echoed.

The toplevelprog echoprog echoopt flags are:
 -n=false
   Do not output trailing newline

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "echoprog", "topic3"},
			Stdout: `Help topic 3 long.
`,
		},
		{
			Args: []string{"echoprog", "help", "topic3"},
			Stdout: `Help topic 3 long.
`,
		},
		{
			Args: []string{"help", "hello"},
			Stdout: `Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   toplevelprog hello [strings]

[strings] are arbitrary strings that will be printed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "foo"},
			Err:  errUsageStr,
			Stderr: `ERROR: toplevelprog: unknown command or topic "foo"

Toplevelprog has the echo subprogram and the hello command.

Usage:
   toplevelprog [flags] <command>

The toplevelprog commands are:
   echoprog    Set of echo commands
   hello       Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "toplevelprog help [command]" for command usage.

The toplevelprog additional help topics are:
   topic1      Help topic 1 short
   topic2      Help topic 2 short
Run "toplevelprog help [topic]" for topic details.

The toplevelprog flags are:
 -tlextra=false
   Print an extra arg for all commands

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args:   []string{"echoprog", "echo", "foo", "bar"},
			Stdout: "[foo bar]\n",
		},
		{
			Args:   []string{"echoprog", "-extra", "echo", "foo", "bar"},
			Stdout: "[foo bar extra]\n",
		},
		{
			Args: []string{"echoprog", "echo", "error"},
			Err:  errEchoStr,
		},
		{
			Args:   []string{"echoprog", "echoopt", "foo", "bar"},
			Stdout: "[foo bar]\n",
		},
		{
			Args:   []string{"echoprog", "-extra", "echoopt", "foo", "bar"},
			Stdout: "[foo bar extra]\n",
		},
		{
			Args:   []string{"echoprog", "echoopt", "-n", "foo", "bar"},
			Stdout: "[foo bar]",
		},
		{
			Args:   []string{"echoprog", "-extra", "echoopt", "-n", "foo", "bar"},
			Stdout: "[foo bar extra]",
		},
		{
			Args: []string{"echoprog", "echoopt", "error"},
			Err:  errEchoStr,
		},
		{
			Args:   []string{"--tlextra", "echoprog", "-extra", "echoopt", "foo", "bar"},
			Stdout: "[foo bar extra tlextra]\n",
		},
		{
			Args:   []string{"hello", "foo", "bar"},
			Stdout: "Hello foo bar\n",
		},
		{
			Args:   []string{"--tlextra", "hello", "foo", "bar"},
			Stdout: "Hello foo bar tlextra\n",
		},
		{
			Args: []string{"hello", "--extra", "foo", "bar"},
			Err:  errUsageStr,
			Stderr: `ERROR: toplevelprog hello: flag provided but not defined: -extra

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   toplevelprog hello [strings]

[strings] are arbitrary strings that will be printed.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"-extra", "echoprog", "echoopt", "foo", "bar"},
			Err:  errUsageStr,
			Stderr: `ERROR: toplevelprog: flag provided but not defined: -extra

Toplevelprog has the echo subprogram and the hello command.

Usage:
   toplevelprog [flags] <command>

The toplevelprog commands are:
   echoprog    Set of echo commands
   hello       Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "toplevelprog help [command]" for command usage.

The toplevelprog additional help topics are:
   topic1      Help topic 1 short
   topic2      Help topic 2 short
Run "toplevelprog help [topic]" for topic details.

The toplevelprog flags are:
 -tlextra=false
   Print an extra arg for all commands

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
	}
	runTestCases(t, prog, tests)
}

func TestMultiLevelCommandsOrdering(t *testing.T) {
	cmdHello11 := &Command{
		Name:  "hello11",
		Short: "Print strings on stdout preceded by \"Hello\"",
		Long: `
Hello prints any strings passed in to stdout preceded by "Hello".
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be printed.",
		Runner:   RunnerFunc(runHello),
	}
	cmdHello12 := &Command{
		Name:  "hello12",
		Short: "Print strings on stdout preceded by \"Hello\"",
		Long: `
Hello prints any strings passed in to stdout preceded by "Hello".
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be printed.",
		Runner:   RunnerFunc(runHello),
	}
	cmdHello21 := &Command{
		Name:  "hello21",
		Short: "Print strings on stdout preceded by \"Hello\"",
		Long: `
Hello prints any strings passed in to stdout preceded by "Hello".
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be printed.",
		Runner:   RunnerFunc(runHello),
	}
	cmdHello22 := &Command{
		Name:  "hello22",
		Short: "Print strings on stdout preceded by \"Hello\"",
		Long: `
Hello prints any strings passed in to stdout preceded by "Hello".
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be printed.",
		Runner:   RunnerFunc(runHello),
	}
	cmdHello31 := &Command{
		Name:  "hello31",
		Short: "Print strings on stdout preceded by \"Hello\"",
		Long: `
Hello prints any strings passed in to stdout preceded by "Hello".
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be printed.",
		Runner:   RunnerFunc(runHello),
	}
	cmdHello32 := &Command{
		Name:  "hello32",
		Short: "Print strings on stdout preceded by \"Hello\"",
		Long: `
Hello prints any strings passed in to stdout preceded by "Hello".
`,
		ArgsName: "[strings]",
		ArgsLong: "[strings] are arbitrary strings that will be printed.",
		Runner:   RunnerFunc(runHello),
	}
	progHello3 := &Command{
		Name:     "prog3",
		Short:    "Set of hello commands",
		Long:     "Prog3 has two variants of hello.",
		Children: []*Command{cmdHello31, cmdHello32},
	}
	progHello2 := &Command{
		Name:     "prog2",
		Short:    "Set of hello commands",
		Long:     "Prog2 has two variants of hello and a subprogram prog3.",
		Children: []*Command{cmdHello21, progHello3, cmdHello22},
	}
	progHello1 := &Command{
		Name:     "prog1",
		Short:    "Set of hello commands",
		Long:     "Prog1 has two variants of hello and a subprogram prog2.",
		Children: []*Command{cmdHello11, cmdHello12, progHello2},
	}

	var tests = []testCase{
		{
			Args: []string{},
			Err:  errUsageStr,
			Stderr: `ERROR: prog1: no command specified

Prog1 has two variants of hello and a subprogram prog2.

Usage:
   prog1 <command>

The prog1 commands are:
   hello11     Print strings on stdout preceded by "Hello"
   hello12     Print strings on stdout preceded by "Hello"
   prog2       Set of hello commands
   help        Display help for commands or topics
Run "prog1 help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help"},
			Stdout: `Prog1 has two variants of hello and a subprogram prog2.

Usage:
   prog1 <command>

The prog1 commands are:
   hello11     Print strings on stdout preceded by "Hello"
   hello12     Print strings on stdout preceded by "Hello"
   prog2       Set of hello commands
   help        Display help for commands or topics
Run "prog1 help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "..."},
			Stdout: `Prog1 has two variants of hello and a subprogram prog2.

Usage:
   prog1 <command>

The prog1 commands are:
   hello11     Print strings on stdout preceded by "Hello"
   hello12     Print strings on stdout preceded by "Hello"
   prog2       Set of hello commands
   help        Display help for commands or topics
Run "prog1 help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Prog1 hello11

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 hello11 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 hello12

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 hello12 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2

Prog2 has two variants of hello and a subprogram prog3.

Usage:
   prog1 prog2 <command>

The prog1 prog2 commands are:
   hello21     Print strings on stdout preceded by "Hello"
   prog3       Set of hello commands
   hello22     Print strings on stdout preceded by "Hello"
================================================================================
Prog1 prog2 hello21

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 hello21 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3

Prog3 has two variants of hello.

Usage:
   prog1 prog2 prog3 <command>

The prog1 prog2 prog3 commands are:
   hello31     Print strings on stdout preceded by "Hello"
   hello32     Print strings on stdout preceded by "Hello"
================================================================================
Prog1 prog2 prog3 hello31

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello31 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3 hello32

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello32 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 hello22

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 hello22 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   prog1 help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The prog1 help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
		{
			Args: []string{"prog2", "help", "..."},
			Stdout: `Prog2 has two variants of hello and a subprogram prog3.

Usage:
   prog1 prog2 <command>

The prog1 prog2 commands are:
   hello21     Print strings on stdout preceded by "Hello"
   prog3       Set of hello commands
   hello22     Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "prog1 prog2 help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Prog1 prog2 hello21

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 hello21 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3

Prog3 has two variants of hello.

Usage:
   prog1 prog2 prog3 <command>

The prog1 prog2 prog3 commands are:
   hello31     Print strings on stdout preceded by "Hello"
   hello32     Print strings on stdout preceded by "Hello"
================================================================================
Prog1 prog2 prog3 hello31

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello31 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3 hello32

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello32 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 hello22

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 hello22 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   prog1 prog2 help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The prog1 prog2 help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
		{
			Args: []string{"prog2", "prog3", "help", "..."},
			Stdout: `Prog3 has two variants of hello.

Usage:
   prog1 prog2 prog3 <command>

The prog1 prog2 prog3 commands are:
   hello31     Print strings on stdout preceded by "Hello"
   hello32     Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "prog1 prog2 prog3 help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Prog1 prog2 prog3 hello31

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello31 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3 hello32

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello32 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3 help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   prog1 prog2 prog3 help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The prog1 prog2 prog3 help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
		{
			Args: []string{"help", "prog2", "prog3", "..."},
			Stdout: `Prog3 has two variants of hello.

Usage:
   prog1 prog2 prog3 <command>

The prog1 prog2 prog3 commands are:
   hello31     Print strings on stdout preceded by "Hello"
   hello32     Print strings on stdout preceded by "Hello"
   help        Display help for commands or topics
Run "prog1 prog2 prog3 help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
================================================================================
Prog1 prog2 prog3 hello31

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello31 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3 hello32

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello32 [strings]

[strings] are arbitrary strings that will be printed.
================================================================================
Prog1 prog2 prog3 help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   prog1 prog2 prog3 help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The prog1 prog2 prog3 help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=80
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
		{
			Args: []string{"help", "-style=godoc", "..."},
			Stdout: `Prog1 has two variants of hello and a subprogram prog2.

Usage:
   prog1 <command>

The prog1 commands are:
   hello11     Print strings on stdout preceded by "Hello"
   hello12     Print strings on stdout preceded by "Hello"
   prog2       Set of hello commands
   help        Display help for commands or topics

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2

Prog1 hello11

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 hello11 [strings]

[strings] are arbitrary strings that will be printed.

Prog1 hello12

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 hello12 [strings]

[strings] are arbitrary strings that will be printed.

Prog1 prog2

Prog2 has two variants of hello and a subprogram prog3.

Usage:
   prog1 prog2 <command>

The prog1 prog2 commands are:
   hello21     Print strings on stdout preceded by "Hello"
   prog3       Set of hello commands
   hello22     Print strings on stdout preceded by "Hello"

Prog1 prog2 hello21

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 hello21 [strings]

[strings] are arbitrary strings that will be printed.

Prog1 prog2 prog3

Prog3 has two variants of hello.

Usage:
   prog1 prog2 prog3 <command>

The prog1 prog2 prog3 commands are:
   hello31     Print strings on stdout preceded by "Hello"
   hello32     Print strings on stdout preceded by "Hello"

Prog1 prog2 prog3 hello31

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello31 [strings]

[strings] are arbitrary strings that will be printed.

Prog1 prog2 prog3 hello32

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 prog3 hello32 [strings]

[strings] are arbitrary strings that will be printed.

Prog1 prog2 hello22

Hello prints any strings passed in to stdout preceded by "Hello".

Usage:
   prog1 prog2 hello22 [strings]

[strings] are arbitrary strings that will be printed.

Prog1 help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   prog1 help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The prog1 help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=<terminal width>
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
`,
		},
	}
	runTestCases(t, progHello1, tests)
}

func TestLongCommands(t *testing.T) {
	cmdLong := &Command{
		Name:   "thisisaverylongcommand",
		Short:  "the short description of the very long command is very long, and will have to be wrapped",
		Long:   "The long description of the very long command is also very long, and will similarly have to be wrapped",
		Runner: RunnerFunc(runEcho),
	}
	cmdShort := &Command{
		Name:   "x",
		Short:  "description of short command.",
		Long:   "blah blah blah",
		Runner: RunnerFunc(runEcho),
	}
	prog := &Command{
		Name:     "program",
		Short:    "Test help strings when there are long commands.",
		Long:     "Test help strings when there are long commands.",
		Children: []*Command{cmdShort, cmdLong},
	}
	var tests = []testCase{
		{
			Args: []string{"help"},
			Stdout: `Test help strings when there are long commands.

Usage:
   program <command>

The program commands are:
   x                      description of short command.
   thisisaverylongcommand the short description of the very long command is very
                          long, and will have to be wrapped
   help                   Display help for commands or topics
Run "program help [command]" for command usage.

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
		{
			Args: []string{"help", "thisisaverylongcommand"},
			Stdout: `The long description of the very long command is also very long, and will
similarly have to be wrapped

Usage:
   program thisisaverylongcommand

The global flags are:
 -global1=
   global test flag 1
 -global2=0
   global test flag 2
`,
		},
	}
	runTestCases(t, prog, tests)
}

func TestHideGlobalFlags(t *testing.T) {
	HideGlobalFlagsExcept(regexp.MustCompile(`^global2$`))
	cmdChild := &Command{
		Name:   "child",
		Short:  "description of child command.",
		Long:   "blah blah blah",
		Runner: RunnerFunc(runEcho),
	}
	prog := &Command{
		Name:     "program",
		Short:    "Test hiding global flags.",
		Long:     "Test hiding global flags.",
		Children: []*Command{cmdChild},
	}
	var tests = []testCase{
		{
			Args: []string{"help"},
			Stdout: `Test hiding global flags.

Usage:
   program <command>

The program commands are:
   child       description of child command.
   help        Display help for commands or topics
Run "program help [command]" for command usage.

The global flags are:
 -global2=0
   global test flag 2

Run "program help -style=full" to show all global flags.
`,
		},
		{
			Args: []string{"help", "child"},
			Stdout: `blah blah blah

Usage:
   program child

The global flags are:
 -global2=0
   global test flag 2

Run "program help -style=full child" to show all global flags.
`,
		},
		{
			Args: []string{"help", "-style=full"},
			Stdout: `Test hiding global flags.

Usage:
   program <command>

The program commands are:
   child       description of child command.
   help        Display help for commands or topics
Run "program help [command]" for command usage.

The global flags are:
 -global2=0
   global test flag 2

 -global1=
   global test flag 1
`,
		},
		{
			Args: []string{"help", "-style=full", "child"},
			Stdout: `blah blah blah

Usage:
   program child

The global flags are:
 -global2=0
   global test flag 2

 -global1=
   global test flag 1
`,
		},
	}
	runTestCases(t, prog, tests)
	nonHiddenGlobalFlags = nil
}

func TestHideGlobalFlagsRootNoChildren(t *testing.T) {
	HideGlobalFlagsExcept(regexp.MustCompile(`^global2$`))
	prog := &Command{
		Name:   "program",
		Short:  "Test hiding global flags, root no children.",
		Long:   "Test hiding global flags, root no children.",
		Runner: RunnerFunc(runEcho),
	}
	var tests = []testCase{
		{
			Args: []string{"-help"},
			Stdout: `Test hiding global flags, root no children.

Usage:
   program

The global flags are:
 -global2=0
   global test flag 2

Run "CMDLINE_STYLE=full program -help" to show all global flags.
`,
		},
		{
			Args: []string{"-help"},
			Envs: map[string]string{"CMDLINE_STYLE": "full"},
			Stdout: `Test hiding global flags, root no children.

Usage:
   program

The global flags are:
 -global2=0
   global test flag 2

 -global1=
   global test flag 1
`,
		},
	}
	runTestCases(t, prog, tests)
	nonHiddenGlobalFlags = nil
}

func TestRootCommandFlags(t *testing.T) {
	root := &Command{
		Name:   "root",
		Short:  "Test root command flags.",
		Long:   "Test root command flags.",
		Runner: RunnerFunc(runHello),
	}
	rb := root.Flags.Bool("rbool", false, "rbool desc")
	rs := root.Flags.String("rstring", "abc", "rstring desc")
	origFlags := flag.CommandLine
	// Parse and make sure the flags get set appropriately.
	_, _, err := Parse(root, NewEnv(), []string{"-rbool=true", "-rstring=XYZ"})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got, want := *rb, true; got != want {
		t.Errorf("rbool got %v want %v", got, want)
	}
	if got, want := *rs, "XYZ"; got != want {
		t.Errorf("rstring got %v want %v", got, want)
	}
	// Make sure we haven't changed the flag.CommandLine pointer, and that it's
	// parsed, and it contains our root command flags.  These properties are
	// important to ensure so that users can check whether the flags are already
	// parsed to avoid double-parsing.  Even if they do call flag.Parse it'll
	// succeed, as long as cmdline.Parse succeeded.
	if got, want := flag.CommandLine, origFlags; got != want {
		t.Errorf("flag.CommandLine pointer changed, got %p want %p", got, want)
	}
	if got, want := flag.CommandLine.Parsed(), true; got != want {
		t.Errorf("flag.CommandLine.Parsed() got %v, want %v", got, want)
	}
	if name := "rbool"; flag.CommandLine.Lookup(name) == nil {
		t.Errorf("flag.CommandLine.Lookup(%q) failed", name)
	}
	if name := "rstring"; flag.CommandLine.Lookup(name) == nil {
		t.Errorf("flag.CommandLine.Lookup(%q) failed", name)
	}
	// Actually try double-parsing flag.CommandLine.
	if err := flag.CommandLine.Parse([]string{"-rbool=false", "-rstring=123"}); err != nil {
		t.Errorf("flag.CommandLine.Parse() failed: %v", err)
	}
	if got, want := *rb, false; got != want {
		t.Errorf("rbool got %v want %v", got, want)
	}
	if got, want := *rs, "123"; got != want {
		t.Errorf("rstring got %v want %v", got, want)
	}
}
