import {spawn} from 'node:child_process'
import {setTimeout as wait} from 'node:timers/promises'
import {fileURLToPath} from 'node:url'
import {existsSync} from 'node:fs'

const cwd=fileURLToPath(new URL('..',import.meta.url))
const children=[]
let closing=false
// stop 仅停止本脚本创建的进程组，绝不终止复用的已有服务。
function stop(code=0){
 if(closing)return
 closing=true
 for(const child of children){try{process.kill(-child.pid,'SIGTERM')}catch(error){if(error.code!=='ESRCH')console.error('停止开发进程失败',error.message)}}
 process.exitCode=code
}
// launch 使用独立进程组管理 Go 与 Vite 的完整子进程生命周期。
function launch(command,args){
 const child=spawn(command,args,{cwd,stdio:'inherit',detached:true})
 children.push(child)
 child.on('error',error=>{console.error('开发服务启动失败：',error.message);stop(1)})
 child.on('exit',code=>{if(!closing)stop(code||0)})
 return child
}
// health 只复用明确处于 dev 模式的健康引擎。
async function health(){
 try{const response=await fetch('http://127.0.0.1:8080/healthz',{signal:AbortSignal.timeout(1500)});const payload=await response.json();return response.ok&&payload.data?.mode==='dev'}
 catch{return false}
}
// main 统一启动前后端，并在控制台给出可直接打开的开发入口。
async function main(){
 if(!existsSync(cwd+'/web/node_modules'))throw new Error('请先运行 npm --prefix web ci 安装前端依赖')
 if(await health())console.log('复用已有 dev 引擎：127.0.0.1:8080（后端代码变更需先重启该实例）')
 else{
  const backend=launch('go',['run','./cmd/engine','--mode','dev'])
  let ready=false
  for(let i=0;i<90&&!closing;i++){if(await health()){ready=true;break}if(backend.exitCode!==null)break;await wait(1000)}
  if(!ready)throw new Error('引擎未就绪，请检查 PostgreSQL、.env 和 8080 端口')
 }
 console.log('\n控制台 dev 热更新入口：http://127.0.0.1:5174\n后端 API： http://127.0.0.1:8080\n按 Ctrl+C 停止本次启动的进程，已有复用服务保持运行。\n')
 launch('npm',['--prefix','web','run','dev'])
}
process.on('SIGINT',()=>stop())
process.on('SIGTERM',()=>stop())
await main().catch(error=>{console.error(error.message);stop(1)})
