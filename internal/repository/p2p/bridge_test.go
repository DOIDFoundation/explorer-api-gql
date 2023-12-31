// Package p2p implements basic P2P communication module to aid connecting Opera nodes
// and extracting basic status information from them.
package p2p

import (
	"fantom-api-graphql/internal/config"
	"fantom-api-graphql/internal/logger"
	"github.com/ethereum/go-ethereum/crypto"
	"testing"
)

// MockBlockHeightProvider mocks block height provider interface.
type MockBlockHeightProvider struct {
}

func (bhp *MockBlockHeightProvider) BlockHeight() uint64 {
	return 37676547
}

func setupP2PTest(_ *testing.T) func(t *testing.T) {
	// bitter bundle shaft slogan spirit unlock soul gaze fun sister ozone better
	pk, _ := crypto.HexToECDSA("c365b9700e2de3aa49c7e7bd48e086113591b817c92567e03460ef27434df8d9")

	// mock-up a config
	SetConfig(&config.Config{
		AppName: "Fantom/GraphQL-API",
		Signature: config.ServerSignature{
			Address:    crypto.PubkeyToAddress(pk.PublicKey),
			PrivateKey: pk,
		},
		Log: config.Log{
			Level:  "DEBUG",
			Format: "%{color}%{shortfunc}%{color:reset}: %{message}",
		},
	})

	// mockup the logger
	SetLogger(logger.New(cfg))

	return func(_ *testing.T) {
		// nothing to tear down
	}
}

func TestPeerInformation(t *testing.T) {
	return
	/*
		defer setupP2PTest(t)(t)
		g := gomega.NewGomegaWithT(t)

		peers := []string{
			// "enode://ae677f9d6fae73650d249923e86adec732174adeff7221ec0953967782ff12432b8863651b78413f4ba234bc925e03ebed21465556076e560a097a66c821b140@65.109.114.156:5050",
			// "enode://4d3e983641f8218862eb367db58b99df4e7a568477859f56d0c0e824dfa5d9f78a0fb0675ace9ed7778bfb35dcd5a852441d27858f95323a15935975492250bc@85.195.103.153:5050",
			// "enode://f14381f5dd20f2ecb0ed61179ae223b5ad2c22249ab4d580c0cb7e62a3e9bdd7d948ea6d905493d6bd42e4827c6239f59e7e36cc0cfaa15950577f19a0b27b9b@135.181.220.15:5050",
			// "enode://06d35b7a537ada8b0a32f909e7228e2a458424b4c323e79ee24c22980192a9ace90fd05830bc2741588f73f43685305cb032d231e986220a4b3af7b7d8f447a7@95.179.220.89:35050",
		}

		var info *types.OperaNodeInformation
		var err error

		// decode a target peer
		for _, adr := range peers {
			info, err = PeerInformation(enode.MustParse(adr), &MockBlockHeightProvider{})
			if err == nil {
				break
			}
		}

		g.Expect(err).To(gomega.BeNil(), err.Error())
		g.Expect(info.Name).To(gomega.ContainSubstring("opera"))
	*/
}
