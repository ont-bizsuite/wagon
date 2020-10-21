package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"

	"github.com/ont-bizsuite/wagon/exec"
	"github.com/ont-bizsuite/wagon/wasm"
)

// this file is based on github.com/perlin-network/life/spec/test_runner/runner.go

type Config struct {
	SourceFilename string    `json:"source_filename"`
	Commands       []Command `json:"commands"`
}

type Command struct {
	Type       string      `json:"type"`
	Line       int         `json:"line"`
	Filename   string      `json:"filename"`
	Name       string      `json:"name"`
	Action     CmdAction   `json:"action"`
	Text       string      `json:"text"`
	ModuleType string      `json:"module_type"`
	Expected   []ValueInfo `json:"expected"`
}

type CmdAction struct {
	Type     string      `json:"type"`
	Module   string      `json:"module"`
	Field    string      `json:"field"`
	Args     []ValueInfo `json:"args"`
	Expected []ValueInfo `json:"expected"`
}

type ValueInfo struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func LoadConfigFromFile(filename string) *Config {
	raw, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	var cfg Config
	err = json.Unmarshal(raw, &cfg)
	if err != nil {
		panic(err)
	}
	return &cfg
}

func (c *Config) Run(cfgPath string) {
	var vm *exec.VM
	namedVMs := make(map[string]*exec.VM)

	dir, _ := filepath.Split(cfgPath)

	for _, cmd := range c.Commands {
		switch cmd.Type {
		case "module":
			input, err := ioutil.ReadFile(path.Join(dir, cmd.Filename))
			if err != nil {
				panic(err)
			}
			m, err := wasm.ReadModule(bytes.NewBuffer(input), nil)
			if err != nil {
				log.Fatalf("could not read module: %v", err)
			}

			memLimits := uint64(math.MaxUint32)
			if m.Memory != nil && len(m.Memory.Entries) > 0 && m.Memory.Entries[0].Limits.Flags == 1 {
				max := m.Memory.Entries[0].Limits.Maximum
				if max != 160 { // currently max memory size for ontology vm
					memLimits = uint64(max) * 65536
				}
			}

			vm, err = exec.NewVM(m, memLimits)
			if err != nil {
				panic(fmt.Errorf("l%d: %s, could not create VM: %v", cmd.Line, cfgPath, err))
			}
			GasLimit := uint64(10000000000)
			ExecStep := uint64(1000000000000)
			vm.RecoverPanic = true
			vm.ExecMetrics = &exec.Gas{GasPrice: 1, GasLimit: &GasLimit, GasFactor: 5, ExecStep: &ExecStep}
			vm.CallStackDepth = 10000
			if cmd.Name != "" {
				namedVMs[cmd.Name] = vm
			}
		case "assert_return", "action":
			localVM := vm
			if cmd.Action.Module != "" {
				if target, ok := namedVMs[cmd.Action.Module]; ok {
					localVM = target
				} else {
					panic("named module not found")
				}
			}
			if localVM == nil {
				panic("module not found")
			}

			switch cmd.Action.Type {
			case "invoke":
				entryID, ok := localVM.GetExportIndex(cmd.Action.Field)
				if !ok {
					panic("export not found (func)")
				}
				args := make([]uint64, 0)
				for _, arg := range cmd.Action.Args {
					var val uint64
					fmt.Sscanf(arg.Value, "%d", &val)
					args = append(args, val)
				}
				fmt.Printf("Entry = %d, len(args) = %d\n", entryID, len(args))
				ret, err := localVM.ExecCode(int64(entryID), args...)
				if err != nil {
					panic(err)
				}
				if len(cmd.Expected) != 0 {
					var _exp uint64
					fmt.Sscanf(cmd.Expected[0].Value, "%d", &_exp)
					exp := int64(_exp)
					var result int64
					if cmd.Expected[0].Type == "i32" || cmd.Expected[0].Type == "f32" {
						result = int64(ret.(uint32))
						exp = int64(uint32(exp))
					} else {
						result = int64(ret.(uint64))
					}
					if result != exp {
						panic(fmt.Errorf("l%d: %s, ret mismatch: got %d, expected %d", cmd.Line, cfgPath, result, exp))
					}
				}
			case "get":
				globalID, ok := localVM.GetExportIndex(cmd.Action.Field)
				if !ok {
					panic("export not found (global)")
				}
				val, ok := localVM.GetGlobal(globalID)
				if !ok {
					panic("export not found (global)")
				}
				var exp uint64
				fmt.Sscanf(cmd.Expected[0].Value, "%d", &exp)
				if cmd.Expected[0].Type == "i32" || cmd.Expected[0].Type == "f32" {
					val = uint64(uint32(val))
					exp = uint64(uint32(exp))
				}
				if val != exp {
					panic(fmt.Errorf("val mismatch: got %d, expected %d\n", val, exp))
				}
			default:
				panic(cmd.Action.Type)
			}
		case "assert_trap":
			localVM := vm
			if cmd.Action.Module != "" {
				if target, ok := namedVMs[cmd.Action.Module]; ok {
					localVM = target
				} else {
					panic("named module not found")
				}
			}
			if localVM == nil {
				panic("module not found")
			}
			switch cmd.Action.Type {
			case "invoke":
				entryID, ok := localVM.GetExportIndex(cmd.Action.Field)
				if !ok {
					panic("export not found (func)")
				}
				args := make([]uint64, 0)
				for _, arg := range cmd.Action.Args {
					var val uint64
					fmt.Sscanf(arg.Value, "%d", &val)
					args = append(args, val)
				}
				fmt.Printf("Entry = %d, len(args) = %d\n", entryID, len(args))
				_, err := localVM.ExecCode(int64(entryID), args...)
				if err == nil {
					panic(fmt.Errorf("L%d: %s, expect a trap\n", cmd.Line, cfgPath))
				}
			default:
				panic(cmd.Action.Type)
			}

		case "assert_malformed", "assert_invalid", "assert_exhaustion", "assert_unlinkable",
			"assert_return_canonical_nan", "assert_return_arithmetic_nan":
			fmt.Printf("skipping %s\n", cmd.Type)
		default:
			panic(cmd.Type)
		}
		fmt.Printf("PASS L%d: %s\n", cmd.Line, cfgPath)
	}
}

func main() {
	cfg := LoadConfigFromFile(os.Args[1])
	cfg.Run(os.Args[1])
}
