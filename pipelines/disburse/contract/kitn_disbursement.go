// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contract

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// KitnDisbursementMetaData contains all meta data concerning the KitnDisbursement contract.
var KitnDisbursementMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"allowance\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timeLimit\",\"type\":\"uint256\"}],\"name\":\"AllowanceRenewed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"receiver\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"CoinsSpent\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"KITN_ADDRESS\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getWalletBalance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"kitnToken\",\"outputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"myAllowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_user\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_allowance\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"_timeLimit\",\"type\":\"uint256\"}],\"name\":\"renewAllowance\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_receiver\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"spendCoins\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"users\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"userAddress\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"allowance\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validity\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
}

// KitnDisbursementABI is the input ABI used to generate the binding from.
// Deprecated: Use KitnDisbursementMetaData.ABI instead.
var KitnDisbursementABI = KitnDisbursementMetaData.ABI

// KitnDisbursement is an auto generated Go binding around an Ethereum contract.
type KitnDisbursement struct {
	KitnDisbursementCaller     // Read-only binding to the contract
	KitnDisbursementTransactor // Write-only binding to the contract
	KitnDisbursementFilterer   // Log filterer for contract events
}

// KitnDisbursementCaller is an auto generated read-only Go binding around an Ethereum contract.
type KitnDisbursementCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// KitnDisbursementTransactor is an auto generated write-only Go binding around an Ethereum contract.
type KitnDisbursementTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// KitnDisbursementFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type KitnDisbursementFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// KitnDisbursementSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type KitnDisbursementSession struct {
	Contract     *KitnDisbursement // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// KitnDisbursementCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type KitnDisbursementCallerSession struct {
	Contract *KitnDisbursementCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// KitnDisbursementTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type KitnDisbursementTransactorSession struct {
	Contract     *KitnDisbursementTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// KitnDisbursementRaw is an auto generated low-level Go binding around an Ethereum contract.
type KitnDisbursementRaw struct {
	Contract *KitnDisbursement // Generic contract binding to access the raw methods on
}

// KitnDisbursementCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type KitnDisbursementCallerRaw struct {
	Contract *KitnDisbursementCaller // Generic read-only contract binding to access the raw methods on
}

// KitnDisbursementTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type KitnDisbursementTransactorRaw struct {
	Contract *KitnDisbursementTransactor // Generic write-only contract binding to access the raw methods on
}

// NewKitnDisbursement creates a new instance of KitnDisbursement, bound to a specific deployed contract.
func NewKitnDisbursement(address common.Address, backend bind.ContractBackend) (*KitnDisbursement, error) {
	contract, err := bindKitnDisbursement(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &KitnDisbursement{KitnDisbursementCaller: KitnDisbursementCaller{contract: contract}, KitnDisbursementTransactor: KitnDisbursementTransactor{contract: contract}, KitnDisbursementFilterer: KitnDisbursementFilterer{contract: contract}}, nil
}

// NewKitnDisbursementCaller creates a new read-only instance of KitnDisbursement, bound to a specific deployed contract.
func NewKitnDisbursementCaller(address common.Address, caller bind.ContractCaller) (*KitnDisbursementCaller, error) {
	contract, err := bindKitnDisbursement(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &KitnDisbursementCaller{contract: contract}, nil
}

// NewKitnDisbursementTransactor creates a new write-only instance of KitnDisbursement, bound to a specific deployed contract.
func NewKitnDisbursementTransactor(address common.Address, transactor bind.ContractTransactor) (*KitnDisbursementTransactor, error) {
	contract, err := bindKitnDisbursement(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &KitnDisbursementTransactor{contract: contract}, nil
}

// NewKitnDisbursementFilterer creates a new log filterer instance of KitnDisbursement, bound to a specific deployed contract.
func NewKitnDisbursementFilterer(address common.Address, filterer bind.ContractFilterer) (*KitnDisbursementFilterer, error) {
	contract, err := bindKitnDisbursement(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &KitnDisbursementFilterer{contract: contract}, nil
}

// bindKitnDisbursement binds a generic wrapper to an already deployed contract.
func bindKitnDisbursement(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := KitnDisbursementMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_KitnDisbursement *KitnDisbursementRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _KitnDisbursement.Contract.KitnDisbursementCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_KitnDisbursement *KitnDisbursementRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.KitnDisbursementTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_KitnDisbursement *KitnDisbursementRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.KitnDisbursementTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_KitnDisbursement *KitnDisbursementCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _KitnDisbursement.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_KitnDisbursement *KitnDisbursementTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_KitnDisbursement *KitnDisbursementTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.contract.Transact(opts, method, params...)
}

// KITNADDRESS is a free data retrieval call binding the contract method 0x4328ec1d.
//
// Solidity: function KITN_ADDRESS() view returns(address)
func (_KitnDisbursement *KitnDisbursementCaller) KITNADDRESS(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _KitnDisbursement.contract.Call(opts, &out, "KITN_ADDRESS")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// KITNADDRESS is a free data retrieval call binding the contract method 0x4328ec1d.
//
// Solidity: function KITN_ADDRESS() view returns(address)
func (_KitnDisbursement *KitnDisbursementSession) KITNADDRESS() (common.Address, error) {
	return _KitnDisbursement.Contract.KITNADDRESS(&_KitnDisbursement.CallOpts)
}

// KITNADDRESS is a free data retrieval call binding the contract method 0x4328ec1d.
//
// Solidity: function KITN_ADDRESS() view returns(address)
func (_KitnDisbursement *KitnDisbursementCallerSession) KITNADDRESS() (common.Address, error) {
	return _KitnDisbursement.Contract.KITNADDRESS(&_KitnDisbursement.CallOpts)
}

// GetWalletBalance is a free data retrieval call binding the contract method 0x329a27e7.
//
// Solidity: function getWalletBalance() view returns(uint256)
func (_KitnDisbursement *KitnDisbursementCaller) GetWalletBalance(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _KitnDisbursement.contract.Call(opts, &out, "getWalletBalance")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetWalletBalance is a free data retrieval call binding the contract method 0x329a27e7.
//
// Solidity: function getWalletBalance() view returns(uint256)
func (_KitnDisbursement *KitnDisbursementSession) GetWalletBalance() (*big.Int, error) {
	return _KitnDisbursement.Contract.GetWalletBalance(&_KitnDisbursement.CallOpts)
}

// GetWalletBalance is a free data retrieval call binding the contract method 0x329a27e7.
//
// Solidity: function getWalletBalance() view returns(uint256)
func (_KitnDisbursement *KitnDisbursementCallerSession) GetWalletBalance() (*big.Int, error) {
	return _KitnDisbursement.Contract.GetWalletBalance(&_KitnDisbursement.CallOpts)
}

// KitnToken is a free data retrieval call binding the contract method 0x961e681a.
//
// Solidity: function kitnToken() view returns(address)
func (_KitnDisbursement *KitnDisbursementCaller) KitnToken(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _KitnDisbursement.contract.Call(opts, &out, "kitnToken")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// KitnToken is a free data retrieval call binding the contract method 0x961e681a.
//
// Solidity: function kitnToken() view returns(address)
func (_KitnDisbursement *KitnDisbursementSession) KitnToken() (common.Address, error) {
	return _KitnDisbursement.Contract.KitnToken(&_KitnDisbursement.CallOpts)
}

// KitnToken is a free data retrieval call binding the contract method 0x961e681a.
//
// Solidity: function kitnToken() view returns(address)
func (_KitnDisbursement *KitnDisbursementCallerSession) KitnToken() (common.Address, error) {
	return _KitnDisbursement.Contract.KitnToken(&_KitnDisbursement.CallOpts)
}

// MyAllowance is a free data retrieval call binding the contract method 0x69376af8.
//
// Solidity: function myAllowance() view returns(uint256)
func (_KitnDisbursement *KitnDisbursementCaller) MyAllowance(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _KitnDisbursement.contract.Call(opts, &out, "myAllowance")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MyAllowance is a free data retrieval call binding the contract method 0x69376af8.
//
// Solidity: function myAllowance() view returns(uint256)
func (_KitnDisbursement *KitnDisbursementSession) MyAllowance() (*big.Int, error) {
	return _KitnDisbursement.Contract.MyAllowance(&_KitnDisbursement.CallOpts)
}

// MyAllowance is a free data retrieval call binding the contract method 0x69376af8.
//
// Solidity: function myAllowance() view returns(uint256)
func (_KitnDisbursement *KitnDisbursementCallerSession) MyAllowance() (*big.Int, error) {
	return _KitnDisbursement.Contract.MyAllowance(&_KitnDisbursement.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_KitnDisbursement *KitnDisbursementCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _KitnDisbursement.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_KitnDisbursement *KitnDisbursementSession) Owner() (common.Address, error) {
	return _KitnDisbursement.Contract.Owner(&_KitnDisbursement.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_KitnDisbursement *KitnDisbursementCallerSession) Owner() (common.Address, error) {
	return _KitnDisbursement.Contract.Owner(&_KitnDisbursement.CallOpts)
}

// Users is a free data retrieval call binding the contract method 0xa87430ba.
//
// Solidity: function users(address ) view returns(address userAddress, uint256 allowance, uint256 validity)
func (_KitnDisbursement *KitnDisbursementCaller) Users(opts *bind.CallOpts, arg0 common.Address) (struct {
	UserAddress common.Address
	Allowance   *big.Int
	Validity    *big.Int
}, error) {
	var out []interface{}
	err := _KitnDisbursement.contract.Call(opts, &out, "users", arg0)

	outstruct := new(struct {
		UserAddress common.Address
		Allowance   *big.Int
		Validity    *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.UserAddress = *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	outstruct.Allowance = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.Validity = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// Users is a free data retrieval call binding the contract method 0xa87430ba.
//
// Solidity: function users(address ) view returns(address userAddress, uint256 allowance, uint256 validity)
func (_KitnDisbursement *KitnDisbursementSession) Users(arg0 common.Address) (struct {
	UserAddress common.Address
	Allowance   *big.Int
	Validity    *big.Int
}, error) {
	return _KitnDisbursement.Contract.Users(&_KitnDisbursement.CallOpts, arg0)
}

// Users is a free data retrieval call binding the contract method 0xa87430ba.
//
// Solidity: function users(address ) view returns(address userAddress, uint256 allowance, uint256 validity)
func (_KitnDisbursement *KitnDisbursementCallerSession) Users(arg0 common.Address) (struct {
	UserAddress common.Address
	Allowance   *big.Int
	Validity    *big.Int
}, error) {
	return _KitnDisbursement.Contract.Users(&_KitnDisbursement.CallOpts, arg0)
}

// RenewAllowance is a paid mutator transaction binding the contract method 0x260e67a7.
//
// Solidity: function renewAllowance(address _user, uint256 _allowance, uint256 _timeLimit) returns()
func (_KitnDisbursement *KitnDisbursementTransactor) RenewAllowance(opts *bind.TransactOpts, _user common.Address, _allowance *big.Int, _timeLimit *big.Int) (*types.Transaction, error) {
	return _KitnDisbursement.contract.Transact(opts, "renewAllowance", _user, _allowance, _timeLimit)
}

// RenewAllowance is a paid mutator transaction binding the contract method 0x260e67a7.
//
// Solidity: function renewAllowance(address _user, uint256 _allowance, uint256 _timeLimit) returns()
func (_KitnDisbursement *KitnDisbursementSession) RenewAllowance(_user common.Address, _allowance *big.Int, _timeLimit *big.Int) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.RenewAllowance(&_KitnDisbursement.TransactOpts, _user, _allowance, _timeLimit)
}

// RenewAllowance is a paid mutator transaction binding the contract method 0x260e67a7.
//
// Solidity: function renewAllowance(address _user, uint256 _allowance, uint256 _timeLimit) returns()
func (_KitnDisbursement *KitnDisbursementTransactorSession) RenewAllowance(_user common.Address, _allowance *big.Int, _timeLimit *big.Int) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.RenewAllowance(&_KitnDisbursement.TransactOpts, _user, _allowance, _timeLimit)
}

// SpendCoins is a paid mutator transaction binding the contract method 0x978d4e7e.
//
// Solidity: function spendCoins(address _receiver, uint256 _amount) returns()
func (_KitnDisbursement *KitnDisbursementTransactor) SpendCoins(opts *bind.TransactOpts, _receiver common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _KitnDisbursement.contract.Transact(opts, "spendCoins", _receiver, _amount)
}

// SpendCoins is a paid mutator transaction binding the contract method 0x978d4e7e.
//
// Solidity: function spendCoins(address _receiver, uint256 _amount) returns()
func (_KitnDisbursement *KitnDisbursementSession) SpendCoins(_receiver common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.SpendCoins(&_KitnDisbursement.TransactOpts, _receiver, _amount)
}

// SpendCoins is a paid mutator transaction binding the contract method 0x978d4e7e.
//
// Solidity: function spendCoins(address _receiver, uint256 _amount) returns()
func (_KitnDisbursement *KitnDisbursementTransactorSession) SpendCoins(_receiver common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _KitnDisbursement.Contract.SpendCoins(&_KitnDisbursement.TransactOpts, _receiver, _amount)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_KitnDisbursement *KitnDisbursementTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _KitnDisbursement.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_KitnDisbursement *KitnDisbursementSession) Receive() (*types.Transaction, error) {
	return _KitnDisbursement.Contract.Receive(&_KitnDisbursement.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_KitnDisbursement *KitnDisbursementTransactorSession) Receive() (*types.Transaction, error) {
	return _KitnDisbursement.Contract.Receive(&_KitnDisbursement.TransactOpts)
}

// KitnDisbursementAllowanceRenewedIterator is returned from FilterAllowanceRenewed and is used to iterate over the raw logs and unpacked data for AllowanceRenewed events raised by the KitnDisbursement contract.
type KitnDisbursementAllowanceRenewedIterator struct {
	Event *KitnDisbursementAllowanceRenewed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *KitnDisbursementAllowanceRenewedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(KitnDisbursementAllowanceRenewed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(KitnDisbursementAllowanceRenewed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *KitnDisbursementAllowanceRenewedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *KitnDisbursementAllowanceRenewedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// KitnDisbursementAllowanceRenewed represents a AllowanceRenewed event raised by the KitnDisbursement contract.
type KitnDisbursementAllowanceRenewed struct {
	User      common.Address
	Allowance *big.Int
	TimeLimit *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterAllowanceRenewed is a free log retrieval operation binding the contract event 0x7b5d8b8c4a48ab3c4f698feadf9ec108f89b430c109781190da11151c6b66067.
//
// Solidity: event AllowanceRenewed(address indexed user, uint256 allowance, uint256 timeLimit)
func (_KitnDisbursement *KitnDisbursementFilterer) FilterAllowanceRenewed(opts *bind.FilterOpts, user []common.Address) (*KitnDisbursementAllowanceRenewedIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _KitnDisbursement.contract.FilterLogs(opts, "AllowanceRenewed", userRule)
	if err != nil {
		return nil, err
	}
	return &KitnDisbursementAllowanceRenewedIterator{contract: _KitnDisbursement.contract, event: "AllowanceRenewed", logs: logs, sub: sub}, nil
}

// WatchAllowanceRenewed is a free log subscription operation binding the contract event 0x7b5d8b8c4a48ab3c4f698feadf9ec108f89b430c109781190da11151c6b66067.
//
// Solidity: event AllowanceRenewed(address indexed user, uint256 allowance, uint256 timeLimit)
func (_KitnDisbursement *KitnDisbursementFilterer) WatchAllowanceRenewed(opts *bind.WatchOpts, sink chan<- *KitnDisbursementAllowanceRenewed, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _KitnDisbursement.contract.WatchLogs(opts, "AllowanceRenewed", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(KitnDisbursementAllowanceRenewed)
				if err := _KitnDisbursement.contract.UnpackLog(event, "AllowanceRenewed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseAllowanceRenewed is a log parse operation binding the contract event 0x7b5d8b8c4a48ab3c4f698feadf9ec108f89b430c109781190da11151c6b66067.
//
// Solidity: event AllowanceRenewed(address indexed user, uint256 allowance, uint256 timeLimit)
func (_KitnDisbursement *KitnDisbursementFilterer) ParseAllowanceRenewed(log types.Log) (*KitnDisbursementAllowanceRenewed, error) {
	event := new(KitnDisbursementAllowanceRenewed)
	if err := _KitnDisbursement.contract.UnpackLog(event, "AllowanceRenewed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// KitnDisbursementCoinsSpentIterator is returned from FilterCoinsSpent and is used to iterate over the raw logs and unpacked data for CoinsSpent events raised by the KitnDisbursement contract.
type KitnDisbursementCoinsSpentIterator struct {
	Event *KitnDisbursementCoinsSpent // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *KitnDisbursementCoinsSpentIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(KitnDisbursementCoinsSpent)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(KitnDisbursementCoinsSpent)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *KitnDisbursementCoinsSpentIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *KitnDisbursementCoinsSpentIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// KitnDisbursementCoinsSpent represents a CoinsSpent event raised by the KitnDisbursement contract.
type KitnDisbursementCoinsSpent struct {
	Receiver common.Address
	Amount   *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterCoinsSpent is a free log retrieval operation binding the contract event 0x92c3d05f8ba31385c55d3263a9c7de7c1e17e7da9dba4b20246c3c0cf8df88c2.
//
// Solidity: event CoinsSpent(address indexed receiver, uint256 amount)
func (_KitnDisbursement *KitnDisbursementFilterer) FilterCoinsSpent(opts *bind.FilterOpts, receiver []common.Address) (*KitnDisbursementCoinsSpentIterator, error) {

	var receiverRule []interface{}
	for _, receiverItem := range receiver {
		receiverRule = append(receiverRule, receiverItem)
	}

	logs, sub, err := _KitnDisbursement.contract.FilterLogs(opts, "CoinsSpent", receiverRule)
	if err != nil {
		return nil, err
	}
	return &KitnDisbursementCoinsSpentIterator{contract: _KitnDisbursement.contract, event: "CoinsSpent", logs: logs, sub: sub}, nil
}

// WatchCoinsSpent is a free log subscription operation binding the contract event 0x92c3d05f8ba31385c55d3263a9c7de7c1e17e7da9dba4b20246c3c0cf8df88c2.
//
// Solidity: event CoinsSpent(address indexed receiver, uint256 amount)
func (_KitnDisbursement *KitnDisbursementFilterer) WatchCoinsSpent(opts *bind.WatchOpts, sink chan<- *KitnDisbursementCoinsSpent, receiver []common.Address) (event.Subscription, error) {

	var receiverRule []interface{}
	for _, receiverItem := range receiver {
		receiverRule = append(receiverRule, receiverItem)
	}

	logs, sub, err := _KitnDisbursement.contract.WatchLogs(opts, "CoinsSpent", receiverRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(KitnDisbursementCoinsSpent)
				if err := _KitnDisbursement.contract.UnpackLog(event, "CoinsSpent", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseCoinsSpent is a log parse operation binding the contract event 0x92c3d05f8ba31385c55d3263a9c7de7c1e17e7da9dba4b20246c3c0cf8df88c2.
//
// Solidity: event CoinsSpent(address indexed receiver, uint256 amount)
func (_KitnDisbursement *KitnDisbursementFilterer) ParseCoinsSpent(log types.Log) (*KitnDisbursementCoinsSpent, error) {
	event := new(KitnDisbursementCoinsSpent)
	if err := _KitnDisbursement.contract.UnpackLog(event, "CoinsSpent", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
