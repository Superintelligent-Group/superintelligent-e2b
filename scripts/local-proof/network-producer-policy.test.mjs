import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

// Tests evaluate the actual policy template over its deliberately small IAM
// subset, not a second hand-written policy. This is not AWS IAM simulation.
const inputs={bucket:'evidence-test',account:'123456789012',prefix:'network-usage/v1/test',producer:'arn:aws:iam::123456789012:role/client',reader:'arn:aws:iam::123456789012:role/reader'}
const template=readFileSync(new URL('../../iac/provider-aws/modules/network-evidence/policy.json.tftpl',import.meta.url),'utf8')
const rendered=template.replace(/\$\$\{aws:userid\}/g,'__USER_ID__').replace(/\$\{(bucket|account|prefix|producer|reader)\}/g,(_,name)=>inputs[name]).replaceAll('__USER_ID__','${aws:userid}')
const policy=JSON.parse(rendered)
const array=x=>Array.isArray(x)?x:[x]
const match=(pattern,value)=>new RegExp('^'+pattern.replace(/[.+^${}()|[\]\\]/g,'\\$&').replaceAll('*','.*').replaceAll('?','.')+'$').test(value)
const expand=(value,ctx)=>value.replaceAll('${aws:userid}',ctx['aws:userid']??'__MISSING__')
function condition(conditions={},ctx){return Object.entries(conditions).every(([op,entries])=>Object.entries(entries).every(([key,wanted])=>{
 const values=array(wanted),actual=ctx[key]
 switch(op){
 case 'Null':return values.includes(String(actual===undefined))
 case 'ArnEquals':case 'StringEquals':case 'Bool':return values.includes(String(actual))
 case 'ArnNotEquals':case 'StringNotEquals':return !values.includes(String(actual))
 default:throw new Error(`untested IAM condition ${op}`)
 }
}))}
function allows(action,resource,ctx,ambientAllow=false){
 let allowed=ambientAllow
 for(const statement of policy.Statement){
  assert.equal(statement.Principal,'*')
  const actionMatches=statement.Action?array(statement.Action).some(p=>match(p,action)):!array(statement.NotAction).some(p=>match(p,action))
  if(!actionMatches)continue
  const selected=statement.Resource?array(statement.Resource).some(p=>match(expand(p,ctx),resource)):!array(statement.NotResource).some(p=>match(expand(p,ctx),resource))
  if(!selected||!condition(statement.Condition,ctx))continue
  if(statement.Effect==='Deny')return false
  assert.equal(statement.Effect,'Allow');allowed=true
 }
 return allowed
}
const user='AROA12345678901234567:i-0123456789abcdef0'
const own=`arn:aws:s3:::${inputs.bucket}/${inputs.prefix}/${inputs.account}/${user}/segment.jsonl`
const bucket=`arn:aws:s3:::${inputs.bucket}`
const producer={'aws:PrincipalArn':inputs.producer,'aws:userid':user,'ec2:SourceInstanceARN':'arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0','s3:if-none-match':'*','aws:SecureTransport':'true'}

test('own conditional EC2 producer succeeds; cross-prefix, non-EC2 and other role fail even with ambient grants',()=>{
 assert.equal(allows('s3:PutObject',own,producer),true)
 assert.equal(allows('s3:PutObject',own.replace(user,'another-producer'),producer,true),false)
 const nonEC2={...producer};delete nonEC2['ec2:SourceInstanceARN']
 assert.equal(allows('s3:PutObject',own,nonEC2,true),false)
 assert.equal(allows('s3:PutObject',own,{...producer,'aws:PrincipalArn':'arn:aws:iam::123456789012:role/unapproved'},true),false)
})
test('overwrite, destructive and policy-changing producer requests fail despite ambient grants',()=>{
 const noCondition={...producer};delete noCondition['s3:if-none-match']
 assert.equal(allows('s3:PutObject',own,noCondition,true),false)
 assert.equal(allows('s3:PutObject',own,{...producer,'s3:if-none-match':'other'},true),false)
 for(const action of ['DeleteObject','DeleteObjectVersion','PutObjectRetention','PutObjectLegalHold','BypassGovernanceRetention'])assert.equal(allows('s3:'+action,own,producer,true),false,action)
 for(const action of ['PutBucketPolicy','DeleteBucketPolicy','PutBucketVersioning','PutBucketObjectLockConfiguration','PutLifecycleConfiguration','DeleteBucket'])assert.equal(allows('s3:'+action,bucket,producer,true),false,action)
 assert.equal(allows('s3:PutObject',own,{...producer,'aws:SecureTransport':'false'},true),false)
})
test('independent reader can access exact versions only and cannot write or change policy',()=>{
 const reader={'aws:PrincipalArn':inputs.reader,'s3:VersionId':'version-1','aws:SecureTransport':'true'}
 assert.equal(allows('s3:GetObjectVersion',own,reader),true)
 for(const action of ['GetObject','PutObject','DeleteObjectVersion','PutObjectRetention','BypassGovernanceRetention'])assert.equal(allows('s3:'+action,own,reader,true),false,action)
 assert.equal(allows('s3:GetObjectVersion',own,{...reader,'s3:VersionId':'null'},true),false)
 const noVersion={...reader};delete noVersion['s3:VersionId'];assert.equal(allows('s3:GetObjectVersion',own,noVersion,true),false)
 // IAM does not support s3:VersionId for GetObjectRetention. Metadata-only
 // permission is separate; the reader implementation pins its request version.
 assert.equal(allows('s3:GetObjectRetention',own,noVersion),true)
 assert.equal(allows('s3:PutBucketPolicy',bucket,reader,true),false)
 for(const action of ['PutObjectTagging','PutEncryptionConfiguration','PutBucketPublicAccessBlock','PutBucketOwnershipControls','PutReplicationConfiguration','RestoreObject'])assert.equal(allows('s3:'+action,action.includes('Object')?own:bucket,reader,true),false,action)
})
