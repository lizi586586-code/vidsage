// Run inside the bridge after the operator has verified the official login.
// Pinning is exclusive: later logins cannot silently replace the generation account.
import {readFile, writeFile} from 'node:fs/promises';
const journal = process.env.OPENMAIC_JOB_JOURNAL;
try {
  if (!journal) throw Error();
  const entries = JSON.parse(await readFile(`${journal}.identity`, 'utf8'));
  if (!Array.isArray(entries) || !entries.some(([name, value]) => name === 'auth_token' && value)) throw Error();
  const response = await fetch('https://open.maic.chat/api/credits', {
    headers: {cookie: entries.map(([name, value]) => `${name}=${value}`).join('; ')},
    redirect: 'error', signal: AbortSignal.timeout(15000),
  });
  const account = await response.json();
  if (!response.ok || account.success === false || !/^user:[A-Za-z0-9_-]{1,128}$/.test(account.subjectKey)) throw Error();
  const path = `${journal}.account`;
  try {
    await writeFile(path, JSON.stringify({subject_key: account.subjectKey}), {flag:'wx', mode:0o600});
  } catch (error) {
    if (error.code !== 'EEXIST') throw error;
    const existing = JSON.parse(await readFile(path, 'utf8'));
    if (existing.subject_key !== account.subjectKey) throw Error();
  }
  console.log(JSON.stringify({success:true,authenticated:true,account_pinned:true}));
} catch {
  console.error(JSON.stringify({success:false,error:'service_account_pin_failed'}));
  process.exitCode=1;
}
