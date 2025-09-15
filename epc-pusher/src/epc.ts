
import { ethers } from 'ethers'



export function getEpcAddress(key: string) {
  // Just return a dummy (but deterministic) epc address for now
  // TODO: get address from actual contract using webevm
  
  let hash = ethers.hashMessage(`salt4-${key}`)
  return ethers.computeAddress(hash)
}
