
const API_URL = 'https://chatapi.blockscan.com/v1/api'



export async function sendChat(address: string, msg: string) {

  let params = {
    method: "sendchat",
    apikey: process.env.BLOCKSCAN_CHAT_API_KEY,
    to: address,
    msg,
    replytoid: "0"
  }

  let r = await fetch(API_URL, {
    method: "POST",
    body: new URLSearchParams(params)
  })

  if (r.status != 200) {
    console.error("blockscan sendchat method got non 200 response:", r)
    throw "blockscan sendchat api error"
  }

  let data = await r.json()
  if (data.message != "OK") {
    console.error("blockscan sendchat strange response:", data)
    throw "blockscan sendchat api error"
  }

  return data.result
}

