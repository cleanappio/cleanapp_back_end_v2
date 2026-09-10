"""Exercise sort selection, exclusions and exactly-once rewards in the CI database."""
import json
import subprocess
import sys
import urllib.error
import urllib.request
from urllib.parse import urlencode

compose = sys.argv[1]
def sql(query):
    return subprocess.check_output(['docker','compose','-f',compose,'exec','-T','mysql','mysql','-uroot','-proot','cleanapp','-Nse',query], text=True).strip()

def request(path, body=None):
    req = urllib.request.Request('http://localhost:18082/api/v3/reports/sort/'+path,
        data=json.dumps(body).encode() if body is not None else None,
        headers={'Content-Type':'application/json'})
    try:
        with urllib.request.urlopen(req,timeout=5) as response:
            return response.status,json.load(response)
    except urllib.error.HTTPError as error:
        return error.code,json.load(error)

png='iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5+1WQAAAAASUVORK5CYII='
ids=[]
for name,owner,image in [('first','ci-author',png),('second','ci-author',png),('own','ci-sorter',png),('empty','ci-author','')]:
    seq=int(sql(f"INSERT INTO reports(public_id,id,team,latitude,longitude,image,description) VALUES ('rpt_sort_{name}','{owner}',1,47.36,8.55,FROM_BASE64('{image}'),'sort fixture'); SELECT LAST_INSERT_ID();"))
    sql(f"INSERT INTO report_analysis(seq,source,classification,language,is_valid) VALUES({seq},'ci','physical','en',TRUE);")
    ids.append(seq)

first,second,own,empty=ids
status,result=request('next?'+urlencode({'sorter_id':'ci-sorter','exclude_report_seqs':first}))
assert status==200 and result['report']['seq']==second,(status,result)
status,result=request('next?'+urlencode({'sorter_id':'ci-sorter','exclude_report_seqs':f'{first},{second}'}))
assert status==404,(status,result) # own and image-less reports must not be offered
sql(f"INSERT INTO report_status(seq,status) VALUES({second},'resolved');")
status,result=request('next?'+urlencode({'sorter_id':'ci-sorter','exclude_report_seqs':first}))
assert status==404,(status,result) # cached IDs must still respect current status
sql(f"UPDATE report_status SET status='active' WHERE seq={second};")
vote={'sorter_id':'ci-sorter','report_seq':second,'verdict':'high_value','urgency_score':2}
assert request('submit',vote)[0]==200
assert request('submit',vote)[0]==409
assert sql("SELECT kitns_daily FROM users WHERE id='ci-sorter'")=='1'
vote.update(sorter_id='ci-sorter-2',urgency_score=8)
status,result=request('submit',vote)
assert status==200 and result['sort_metrics']['sort_count']==2,(status,result)
assert result['sort_metrics']['urgency_mean']==5,(status,result)
status,result=request('next?'+urlencode({'sorter_id':'ci-sorter'}))
assert status==200 and result['report']['seq']==first,(status,result)
print('Sort selection, exclusions, images, duplicate votes, rewards and urgency averages passed')
