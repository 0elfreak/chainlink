package config_test

import (
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/guregu/null.v4"

	"github.com/smartcontractkit/chainlink/core/assets"
	evmconfig "github.com/smartcontractkit/chainlink/core/chains/evm/config"
	v2 "github.com/smartcontractkit/chainlink/core/chains/evm/config/v2"
	evmtypes "github.com/smartcontractkit/chainlink/core/chains/evm/types"
	"github.com/smartcontractkit/chainlink/core/config"
	"github.com/smartcontractkit/chainlink/core/internal/testutils"
	"github.com/smartcontractkit/chainlink/core/internal/testutils/configtest"
	configtest2 "github.com/smartcontractkit/chainlink/core/internal/testutils/configtest/v2"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/chainlink/core/services/chainlink"
	"github.com/smartcontractkit/chainlink/core/store/models"
	"github.com/smartcontractkit/chainlink/core/utils"
)

func TestChainScopedConfig(t *testing.T) {
	orm := make(fakeChainConfigORM)
	chainID := big.NewInt(rand.Int63())
	gcfg := configtest.NewTestGeneralConfig(t)
	lggr := logger.TestLogger(t).With("evmChainID", chainID.String())
	cfg := evmconfig.NewChainScopedConfig(chainID, evmtypes.ChainCfg{
		KeySpecific:       make(map[string]evmtypes.ChainCfg),
		EvmMaxGasPriceWei: assets.NewWeiI(100000000000000),
	}, orm, lggr, gcfg)

	t.Run("EvmGasPriceDefault", func(t *testing.T) {
		t.Run("sets the gas price", func(t *testing.T) {
			assert.Equal(t, assets.NewWeiI(20000000000), cfg.EvmGasPriceDefault())

			err := cfg.SetEvmGasPriceDefault(big.NewInt(42000000000))
			assert.NoError(t, err)

			assert.Equal(t, assets.NewWeiI(42000000000), cfg.EvmGasPriceDefault())

			got, ok := orm.LoadString(*utils.NewBig(chainID), "EvmGasPriceDefault")
			if assert.True(t, ok) {
				assert.Equal(t, "42000000000", got)
			}
		})
		t.Run("is not allowed to set gas price to below EvmMinGasPriceWei", func(t *testing.T) {
			assert.Equal(t, assets.NewWeiI(1000000000), cfg.EvmMinGasPriceWei())

			err := cfg.SetEvmGasPriceDefault(big.NewInt(1))
			assert.EqualError(t, err, "cannot set default gas price to 1, it is below the minimum allowed value of 1 gwei")

			assert.Equal(t, assets.NewWeiI(42000000000), cfg.EvmGasPriceDefault())
		})
		t.Run("is not allowed to set gas price to above EvmMaxGasPriceWei", func(t *testing.T) {
			assert.Equal(t, assets.NewWeiI(100000000000000), cfg.EvmMaxGasPriceWei())

			err := cfg.SetEvmGasPriceDefault(big.NewInt(999999999999999))
			assert.EqualError(t, err, "cannot set default gas price to 999999999999999, it is above the maximum allowed value of 100 micro")

			assert.Equal(t, assets.NewWeiI(42000000000), cfg.EvmGasPriceDefault())
		})
	})

	t.Run("KeySpecificMaxGasPriceWei", func(t *testing.T) {
		addr := testutils.NewAddress()
		randomOtherAddr := testutils.NewAddress()
		otherKeySpecific := evmtypes.ChainCfg{EvmMaxGasPriceWei: assets.GWei(850)}
		evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
			cfg.KeySpecific[randomOtherAddr.Hex()] = otherKeySpecific
		})

		t.Run("uses chain-specific default value when nothing is set", func(t *testing.T) {
			assert.Equal(t, assets.NewWeiI(100000000000000), cfg.KeySpecificMaxGasPriceWei(addr))
		})

		t.Run("uses chain-specific override value when that is set", func(t *testing.T) {
			val := assets.NewWeiI(rand.Int63())
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.EvmMaxGasPriceWei = val
			})

			assert.Equal(t, val.String(), cfg.KeySpecificMaxGasPriceWei(addr).String())
		})
		t.Run("uses key-specific override value when set", func(t *testing.T) {
			val := assets.GWei(250)
			keySpecific := evmtypes.ChainCfg{EvmMaxGasPriceWei: val}
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.KeySpecific[addr.Hex()] = keySpecific
			})

			assert.Equal(t, val.String(), cfg.KeySpecificMaxGasPriceWei(addr).String())
		})
		t.Run("uses key-specific override value when set and lower than chain specific config", func(t *testing.T) {
			keySpecificPrice := assets.GWei(900)
			chainSpecificPrice := assets.GWei(1200)
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.EvmMaxGasPriceWei = chainSpecificPrice
				cfg.KeySpecific[addr.Hex()] = evmtypes.ChainCfg{EvmMaxGasPriceWei: keySpecificPrice}
			})

			assert.Equal(t, keySpecificPrice.String(), cfg.KeySpecificMaxGasPriceWei(addr).String())
		})
		t.Run("uses chain-specific value when higher than key-specific value", func(t *testing.T) {
			keySpecificPrice := assets.GWei(1400)
			chainSpecificPrice := assets.GWei(1200)
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.EvmMaxGasPriceWei = chainSpecificPrice
				cfg.KeySpecific[addr.Hex()] = evmtypes.ChainCfg{EvmMaxGasPriceWei: keySpecificPrice}
			})

			assert.Equal(t, chainSpecificPrice.String(), cfg.KeySpecificMaxGasPriceWei(addr).String())
		})
		t.Run("uses key-specific override value when set and lower than global config", func(t *testing.T) {
			keySpecificPrice := assets.GWei(900)
			gcfg.Overrides.GlobalEvmMaxGasPriceWei = assets.GWei(1200)
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.KeySpecific[addr.Hex()] = evmtypes.ChainCfg{EvmMaxGasPriceWei: keySpecificPrice}
			})

			assert.Equal(t, keySpecificPrice.String(), cfg.KeySpecificMaxGasPriceWei(addr).String())
		})
		t.Run("uses global value when higher than key-specific value", func(t *testing.T) {
			keySpecificPrice := assets.GWei(1400)
			gcfg.Overrides.GlobalEvmMaxGasPriceWei = assets.GWei(1200)
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.KeySpecific[addr.Hex()] = evmtypes.ChainCfg{EvmMaxGasPriceWei: keySpecificPrice}
			})

			assert.Equal(t, gcfg.Overrides.GlobalEvmMaxGasPriceWei.String(), cfg.KeySpecificMaxGasPriceWei(addr).String())
		})
		t.Run("uses global value when there is no key-specific price", func(t *testing.T) {
			val := assets.NewWeiI(rand.Int63())
			unsetAddr := testutils.NewAddress()
			gcfg.Overrides.GlobalEvmMaxGasPriceWei = val

			assert.Equal(t, val.String(), cfg.KeySpecificMaxGasPriceWei(unsetAddr).String())
		})
	})

	t.Run("LinkContractAddress", func(t *testing.T) {
		t.Run("uses chain-specific default value when nothing is set", func(t *testing.T) {
			assert.Equal(t, "", cfg.LinkContractAddress())
		})

		t.Run("uses chain-specific override value when that is set", func(t *testing.T) {
			val := testutils.NewAddress().String()
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.LinkContractAddress = null.StringFrom(val)
			})

			assert.Equal(t, val, cfg.LinkContractAddress())
		})

		t.Run("uses global value when that is set", func(t *testing.T) {
			val := testutils.NewAddress().String()
			gcfg.Overrides.LinkContractAddress = null.StringFrom(val)

			assert.Equal(t, val, cfg.LinkContractAddress())
		})
	})

	t.Run("OperatorFactoryAddress", func(t *testing.T) {
		t.Run("uses chain-specific default value when nothing is set", func(t *testing.T) {
			assert.Equal(t, "", cfg.OperatorFactoryAddress())
		})

		t.Run("uses chain-specific override value when that is set", func(t *testing.T) {
			val := testutils.NewAddress().String()
			evmconfig.UpdatePersistedCfg(cfg, func(cfg *evmtypes.ChainCfg) {
				cfg.OperatorFactoryAddress = null.StringFrom(val)
			})

			assert.Equal(t, val, cfg.OperatorFactoryAddress())
		})

		t.Run("uses global value when that is set", func(t *testing.T) {
			val := testutils.NewAddress().String()
			gcfg.Overrides.OperatorFactoryAddress = null.StringFrom(val)

			assert.Equal(t, val, cfg.OperatorFactoryAddress())
		})
	})
}

func TestChainScopedConfig_BSCDefaults(t *testing.T) {
	orm := make(fakeChainConfigORM)
	chainID := big.NewInt(56)
	gcfg := configtest.NewTestGeneralConfig(t)
	lggr := logger.TestLogger(t).With("evmChainID", chainID.String())
	cfg := evmconfig.NewChainScopedConfig(chainID, evmtypes.ChainCfg{
		KeySpecific: make(map[string]evmtypes.ChainCfg),
	}, orm, lggr, gcfg)

	timeout := cfg.OCRDatabaseTimeout()
	require.Equal(t, 2*time.Second, timeout)
	timeout = cfg.OCRContractTransmitterTransmitTimeout()
	require.Equal(t, 2*time.Second, timeout)
	timeout = cfg.OCRObservationGracePeriod()
	require.Equal(t, 500*time.Millisecond, timeout)
}

func TestChainScopedConfig_Profiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                           string
		chainID                        int64
		expectedGasLimitDefault        uint32
		expectedMinimumContractPayment string
	}{
		{"default", 0, 500000, "0.00001"},
		{"mainnet", 1, 500000, "0.1"},
		{"kovan", 42, 500000, "0.1"},

		{"optimism", 10, 500000, "0.00001"},
		{"optimism", 69, 500000, "0.00001"},
		{"optimism", 420, 500000, "0.00001"},

		{"bscMainnet", 56, 500000, "0.00001"},
		{"hecoMainnet", 128, 500000, "0.00001"},
		{"fantomMainnet", 250, 500000, "0.00001"},
		{"fantomTestnet", 4002, 500000, "0.00001"},
		{"polygonMatic", 800001, 500000, "0.00001"},
		{"harmonyMainnet", 1666600000, 500000, "0.00001"},
		{"harmonyTestnet", 1666700000, 500000, "0.00001"},

		{"xDai", 100, 500000, "0.00001"},
	}
	for _, test := range tests {
		tt := test

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gcfg := configtest.NewTestGeneralConfig(t)
			lggr := logger.TestLogger(t)
			config := evmconfig.NewChainScopedConfig(big.NewInt(tt.chainID), evmtypes.ChainCfg{}, nil, lggr, gcfg)

			assert.Equal(t, tt.expectedGasLimitDefault, config.EvmGasLimitDefault())
			assert.Nil(t, config.EvmGasLimitOCRJobType())
			assert.Nil(t, config.EvmGasLimitDRJobType())
			assert.Nil(t, config.EvmGasLimitVRFJobType())
			assert.Nil(t, config.EvmGasLimitFMJobType())
			assert.Nil(t, config.EvmGasLimitKeeperJobType())
			assert.Equal(t, tt.expectedMinimumContractPayment, strings.TrimRight(config.MinimumContractPayment().Link(), "0"))
		})
	}
}

func configWithChain(t *testing.T, id int64, chain *v2.Chain) config.GeneralConfig {
	return configtest2.NewGeneralConfig(t, func(c *chainlink.Config, s *chainlink.Secrets) {
		chainID := utils.NewBigI(id)
		c.EVM[0] = &v2.EVMConfig{ChainID: chainID, Enabled: ptr(true), Chain: v2.DefaultsFrom(chainID, chain),
			Nodes: v2.EVMNodes{{Name: ptr("fake"), HTTPURL: models.MustParseURL("http://foo.test")}}}
	})
}

func Test_chainScopedConfig_Validate(t *testing.T) {
	// Validate built-in
	for id := range evmconfig.ChainSpecificConfigDefaultSets() {
		id := id
		t.Run(fmt.Sprintf("chainID-%d", id), func(t *testing.T) {
			cfg := configWithChain(t, id, nil)
			assert.NoError(t, cfg.Validate())
		})
	}

	// Invalid Cases:

	t.Run("arbitrum-estimator", func(t *testing.T) {
		t.Run("custom", func(t *testing.T) {
			cfg := configWithChain(t, 0, &v2.Chain{
				ChainType: ptr(string(config.ChainArbitrum)),
				GasEstimator: v2.GasEstimator{
					Mode: ptr("BlockHistory"),
				},
			})
			assert.NoError(t, cfg.Validate())
		})
		t.Run("mainnet", func(t *testing.T) {
			cfg := configWithChain(t, 42161, &v2.Chain{
				GasEstimator: v2.GasEstimator{
					Mode: ptr("BlockHistory"),
					BlockHistory: v2.BlockHistoryEstimator{
						BlockHistorySize: ptr[uint16](1),
					},
				},
			})
			assert.NoError(t, cfg.Validate())
		})
		t.Run("testnet", func(t *testing.T) {
			cfg := configWithChain(t, 421611, &v2.Chain{
				GasEstimator: v2.GasEstimator{
					Mode: ptr("L2Suggested"),
				},
			})
			assert.NoError(t, cfg.Validate())
		})
	})

	t.Run("optimism-estimator", func(t *testing.T) {
		t.Run("custom", func(t *testing.T) {
			cfg := configWithChain(t, 0, &v2.Chain{
				ChainType: ptr(string(config.ChainOptimism)),
				GasEstimator: v2.GasEstimator{
					Mode: ptr("BlockHistory"),
				},
			})
			assert.Error(t, cfg.Validate())
		})
		t.Run("mainnet", func(t *testing.T) {
			cfg := configWithChain(t, 10, &v2.Chain{
				GasEstimator: v2.GasEstimator{
					Mode: ptr("FixedPrice"),
				},
			})
			assert.Error(t, cfg.Validate())
		})
		t.Run("testnet", func(t *testing.T) {
			cfg := configWithChain(t, 69, &v2.Chain{
				GasEstimator: v2.GasEstimator{
					Mode: ptr("BlockHistory"),
				},
			})
			assert.Error(t, cfg.Validate())
		})
	})
}

type fakeChainConfigORM map[string]map[string]string

func (f fakeChainConfigORM) LoadString(chainID utils.Big, key string) (val string, ok bool) {
	var m map[string]string
	m, ok = f[chainID.String()]
	if ok {
		val, ok = m[key]
	}
	return
}

func (f fakeChainConfigORM) StoreString(chainID utils.Big, key, val string) error {
	m, ok := f[chainID.String()]
	if !ok {
		m = make(map[string]string)
		f[chainID.String()] = m
	}
	m[key] = val
	return nil
}

func (f fakeChainConfigORM) Clear(chainID utils.Big, key string) error {
	m, ok := f[chainID.String()]
	if ok {
		delete(m, key)
	}
	return nil
}

func ptr[T any](t T) *T { return &t }
