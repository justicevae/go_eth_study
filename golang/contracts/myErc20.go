package contracts

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const ERC20ABI = `[{"constant":true,"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_spender","type":"address"},{"name":"_value","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"totalSupply","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_from","type":"address"},{"name":"_to","type":"address"},{"name":"_value","type":"uint256"}],"name":"transferFrom","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"name":"_owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"balance","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_to","type":"address"},{"name":"_value","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"name":"_owner","type":"address"},{"name":"_spender","type":"address"}],"name":"allowance","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"payable":true,"stateMutability":"payable","type":"fallback"},{"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"spender","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"}]`

type ERC20 struct {
	contract *bind.BoundContract
	address  common.Address
}

func NewERC20(address common.Address, backend bind.ContractBackend) (*ERC20, error) {
	parsedABI, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		return nil, err
	}

	boundContract := bind.NewBoundContract(address, parsedABI, backend, backend, backend)

	return &ERC20{
		contract: boundContract,
		address:  address,
	}, nil
}

func (e *ERC20) Address() common.Address {
	return e.address
}

func (e *ERC20) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := e.contract.Call(opts, &out, "name")
	if err != nil {
		return "", err
	}
	return out[0].(string), nil
}

func (e *ERC20) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := e.contract.Call(opts, &out, "symbol")
	if err != nil {
		return "", err
	}
	return out[0].(string), nil
}

func (e *ERC20) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := e.contract.Call(opts, &out, "decimals")
	if err != nil {
		return 0, err
	}
	return out[0].(uint8), nil
}

func (e *ERC20) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := e.contract.Call(opts, &out, "totalSupply")
	if err != nil {
		return nil, err
	}
	return out[0].(*big.Int), nil
}

func (e *ERC20) BalanceOf(opts *bind.CallOpts, owner common.Address) (*big.Int, error) {
	var out []interface{}
	err := e.contract.Call(opts, &out, "balanceOf", owner)
	if err != nil {
		return nil, err
	}
	return out[0].(*big.Int), nil
}

func (e *ERC20) Allowance(opts *bind.CallOpts, owner, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := e.contract.Call(opts, &out, "allowance", owner, spender)
	if err != nil {
		return nil, err
	}
	return out[0].(*big.Int), nil
}

func (e *ERC20) Transfer(opts *bind.TransactOpts, to common.Address, value *big.Int) (*types.Transaction, error) {
	return e.contract.Transact(opts, "transfer", to, value)
}

func (e *ERC20) Approve(opts *bind.TransactOpts, spender common.Address, value *big.Int) (*types.Transaction, error) {
	return e.contract.Transact(opts, "approve", spender, value)
}

func (e *ERC20) TransferFrom(opts *bind.TransactOpts, from, to common.Address, value *big.Int) (*types.Transaction, error) {
	return e.contract.Transact(opts, "transferFrom", from, to, value)
}
