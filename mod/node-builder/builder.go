// SPDX-License-Identifier: MIT
//
// Copyright (c) 2024 Berachain Foundation
//
// Permission is hereby granted, free of charge, to any person
// obtaining a copy of this software and associated documentation
// files (the "Software"), to deal in the Software without
// restriction, including without limitation the rights to use,
// copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following
// conditions:
//
// The above copyright notice and this permission notice shall be
// included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
// OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.

package nodebuilder

import (
	"context"
	"os"

	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	modclient "github.com/berachain/beacon-kit/mod/node-builder/client"
	cmdlib "github.com/berachain/beacon-kit/mod/node-builder/commands"
	"github.com/berachain/beacon-kit/mod/node-builder/commands/utils/tos"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/server"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// AppInfo is a struct that holds the application information.
type AppInfo[T servertypes.Application] struct {
	// Name is the name of the application.
	Name string
	// Description is a short description of the application.
	Description string
	// Creator is a function that creates the application.
	Creator servertypes.AppCreator[T]
	// Config is the configuration for the application.
	Config depinject.Config
}

// NodeBuilder is a struct that holds the application information.
type NodeBuilder[T servertypes.Application] struct {
	// Every node has some application it is running.
	appInfo *AppInfo[T]
}

// NewNodeBuilder creates a new NodeBuilder.
func NewNodeBuilder[T servertypes.Application]() *NodeBuilder[T] {
	return &NodeBuilder[T]{}
}

// Run runs the application.
func (nb *NodeBuilder[T]) RunNode() {
	rootCmd := nb.BuildRootCmd()
	// Run the root command.
	if err := svrcmd.Execute(
		rootCmd, "", modclient.DefaultNodeHome,
	); err != nil {
		log.NewLogger(rootCmd.OutOrStderr()).
			Error("failure when running app", "error", err)
		os.Exit(1)
	}
}

// BuildRootCmd builds the root command for the application.
func (nb *NodeBuilder[T]) BuildRootCmd() *cobra.Command {
	var (
		autoCliOpts autocli.AppOptions
		mm          *module.Manager
		clientCtx   client.Context
	)
	if err := depinject.Inject(
		depinject.Configs(
			nb.appInfo.Config,
			depinject.Supply(
				log.NewNopLogger(),
				simtestutil.NewAppOptionsWithFlagHome(tempDir()),
			),
			depinject.Provide(
				modclient.ProvideClientContext,
				modclient.ProvideKeyring,
			),
		),
		&autoCliOpts,
		&mm,
		&clientCtx,
	); err != nil {
		panic(err)
	}

	rootCmd := &cobra.Command{
		Use:   nb.appInfo.Name,
		Short: nb.appInfo.Description,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// set the default command outputs
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			var err error
			clientCtx, err = client.ReadPersistentCommandFlags(
				clientCtx,
				cmd.Flags(),
			)
			if err != nil {
				return err
			}

			if err = tos.VerifyTosAcceptedOrPrompt(
				nb.appInfo.Name, modclient.TermsOfServiceURL, clientCtx, cmd,
			); err != nil {
				return err
			}

			customClientTemplate, customClientConfig := modclient.InitClientConfig()
			clientCtx, err = config.CreateClientConfig(
				clientCtx,
				customClientTemplate,
				customClientConfig,
			)
			if err != nil {
				return err
			}

			if err = client.SetCmdClientContextHandler(
				clientCtx, cmd,
			); err != nil {
				return err
			}

			customAppTemplate, customAppConfig := modclient.InitAppConfig()
			customCMTConfig := modclient.InitCometBFTConfig()

			return server.InterceptConfigsPreRunHandler(
				cmd,
				customAppTemplate,
				customAppConfig,
				customCMTConfig,
			)
		},
	}

	cmdlib.DefaultRootCommandSetup(
		rootCmd,
		mm,
		nb.appInfo.Creator,
		func(
			_app T,
			_ *server.Context,
			clientCtx client.Context,
			ctx context.Context,
			_ *errgroup.Group,
		) error {
			return interface{}(_app).(BeaconApp).PostStartup(
				ctx,
				clientCtx,
			)
		},
	)

	if err := autoCliOpts.EnhanceRootCommand(rootCmd); err != nil {
		panic(err)
	}

	return rootCmd
}