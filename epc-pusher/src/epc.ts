
import { ethers } from 'ethers'
import { Address, EVM, createWebEVM } from 'webevm'
import epcAbi from '../lib/abi/EPCPure.json'


class LocalContract {
  iface: ethers.Interface
  address: Address
  evm: EVM

  constructor(contract: { bytecode: string, abi: any }) {
    this.evm = createWebEVM()
    let contractCode = Buffer.from(
      ethers.getBytes(contract.bytecode).buffer
    )
    let r = this.evm.runCall({
      data: contractCode,
      gasLimit: BigInt(2 ** 32),
    })

    if (r.execResult.exceptionError) {
      throw `EVM Error: ${JSON.stringify(r.execResult.exceptionError)}`
    }

    this.address = r.createdAddress as any
    this.iface = new ethers.Interface(contract.abi)

    return new Proxy<any>(
      this,
      {
        get(target, prop, receiver) {
          if (typeof(prop) === "symbol" || prop in target) {
            return Reflect.get(target, prop, receiver)
          }
          let f = target.iface.getFunction(prop as string)
          if (f) {
            return (...args: any) => target._call(f, args)
          }
        },
      },
    )
  }

  _call(func: ethers.FunctionFragment, args: any[]): any {

    let data: Buffer
    try {
      let encoded = this.iface.encodeFunctionData(func, args)
      data = Buffer.from(ethers.getBytes(encoded))
    } catch (e) {
      console.log(`error encoding ${func.name}`)
      throw e
    }
    
    let r = this.evm.runPure({ to: this.address, data })

    if (r.execResult.exceptionError) {
      console.log("evm error calling", func.name, args)
      if (r.execResult.returnValue.length) {
        try {
          let err = this.iface.parseError(r.execResult.returnValue)
          console.log("evm error", err)
        } catch (e) {
          console.log("Couldnt decode error", ethers.hexlify(r.execResult.returnValue))
        }
      } else {
        console.log("evm no error data")
      }
      throw r
    }

    let result = this.iface.decodeFunctionResult(
      func,
      r.execResult.returnValue,
    )

    return result
  }
}

const epcPure = new LocalContract(epcAbi) as any

export function getEpcAddress(key: string) {
  return epcPure.epcAddressWithDeployer(key, process.env.EPC_CONTRACT_ADDRESS)[0]
}
