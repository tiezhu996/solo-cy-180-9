#!/bin/bash
# 转写校对模块端到端验证脚本
# 覆盖：正常校对、审核退回、重提、越权、跨项目、录音未就绪、项目归档、并发修改、版本保留
BASE=${BASE:-http://127.0.0.1:9180/api/v1}
PASS=0; FAIL=0

say()  { echo -e "\n\033[1m== $*\033[0m"; }
ok()   { PASS=$((PASS+1)); echo "  \033[32mPASS\033[0m $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  \033[31mFAIL\033[0m $1"; }
check(){ # check <desc> <actual> <expected>
  if [ "$2" == "$3" ]; then ok "$1 ($2)"; else bad "$1: 期望 $3, 实际 $2"; fi
}

# req <method> <path> <token> [json-body]  ->  echoes "HTTP_CODE<TAB>body"
req() {
  local method=$1 path=$2 token=$3 body=$4
  local args=(-s -o /tmp/resp.json -w "%{http_code}" -X "$method" "$BASE$path" -H "Content-Type: application/json")
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-d "$body")
  local code=$(curl "${args[@]}")
  echo "$code"
}

login() { # login <user> <pass> -> token
  req POST /auth/login "" "{\"username\":\"$1\",\"password\":\"$2\"}" >/dev/null
  jq -r '.data.token' /tmp/resp.json
}

say "准备：注册用户与登录"
req POST /auth/register "" '{"username":"iv1","password":"pass123456","display_name":"采访员一","role":"interviewer"}' >/dev/null
req POST /auth/register "" '{"username":"ar1","password":"pass123456","display_name":"档案员一","role":"archivist"}' >/dev/null
IV=$(login iv1 pass123456); AR=$(login ar1 pass123456); AD=$(login admin admin123456)
[ -n "$IV" ] && [ "$IV" != "null" ] && ok "采访员登录" || bad "采访员登录"
[ -n "$AR" ] && [ "$AR" != "null" ] && ok "档案员登录" || bad "档案员登录"

say "准备：项目/问题/录音"
req POST /projects "$IV" '{"title":"抗战老兵口述","interviewee_name":"张老","birth_year":1930,"background":"抗战经历"}' >/dev/null
P1=$(jq '.data.id' /tmp/resp.json)
req PUT /projects/$P1/status "$IV" '{"status":"in_progress"}' >/dev/null
req POST /projects/$P1/questions "$IV" '{"content":"请回忆您的童年"}' >/dev/null
Q1=$(jq '.data.id' /tmp/resp.json)
req POST /projects "$IV" '{"title":"工厂变迁口述","interviewee_name":"李师傅","birth_year":1950}' >/dev/null
P2=$(jq '.data.id' /tmp/resp.json)
req PUT /projects/$P2/status "$IV" '{"status":"in_progress"}' >/dev/null
req POST /projects/$P2/questions "$IV" '{"content":"请介绍当年的车间"}' >/dev/null
Q2=$(jq '.data.id' /tmp/resp.json)

mkrec() { # mkrec <project> <question> <ready:1|0>
  req POST /recordings "$IV" "{\"project_id\":$1,\"question_id\":$2,\"duration_seconds\":120}" >/dev/null
  local rid=$(jq '.data.id' /tmp/resp.json)
  if [ "$3" == "1" ]; then
    printf 'dummy audio bytes' > /tmp/a.webm
    curl -s -o /dev/null -X POST "$BASE/recordings/$rid/audio" -H "Authorization: Bearer $IV" -F "file=@/tmp/a.webm" -F "duration_seconds=120"
  fi
  echo $rid
}
R1=$(mkrec $P1 $Q1 1)   # 就绪：正常流程
R3=$(mkrec $P1 $Q1 0)   # 未就绪
R4=$(mkrec $P1 $Q1 1)   # 就绪：并发测试
R2=$(mkrec $P2 $Q2 1)   # 就绪：跨项目/归档测试
echo "  P1=$P1 P2=$P2 Q1=$Q1 R1=$R1 R2=$R2 R3=$R3 R4=$R4"
st() { req GET /recordings/$1 "$IV" >/dev/null; jq -r '.data.status' /tmp/resp.json; }
check "R1 录音就绪" "$(st $R1)" "ready"
check "R3 录音未就绪" "$(st $R3)" "recording"

say "1. 正常校对流程（草稿→分段→提交→逐段确认→通过→检索/导出）"
CODE=$(req POST /transcripts "$IV" "{\"recording_id\":$R1,\"project_id\":$P1}")
check "创建转写草稿" "$CODE" "200"
T1=$(jq '.data.id' /tmp/resp.json); V=$(jq '.data.version' /tmp/resp.json); ST=$(jq -r '.data.status' /tmp/resp.json)
check "版本为 v1" "$V" "1"; check "初始状态 draft" "$ST" "draft"

CODE=$(req PUT /transcripts/$T1/segments "$IV" '{"segments":[
  {"start_second":0,"end_second":30,"speaker":"张老","content":"我出生在山东的一个小村庄。"},
  {"start_second":30,"end_second":75,"speaker":"采访员","content":"您还记得村里的老槐树吗？"},
  {"start_second":75,"end_second":120,"speaker":"张老","content":"记得，树下是村里开会的地方。"}]}')
check "保存分段(3段)" "$CODE" "200"
SEG1=$(jq '.data.segments[0].id' /tmp/resp.json); SEG2=$(jq '.data.segments[1].id' /tmp/resp.json); SEG3=$(jq '.data.segments[2].id' /tmp/resp.json)

# 提交前检索应为空（未通过不可检索）
req GET "/transcripts/search?q=老槐树&project_id=$P1" "$IV" >/dev/null
check "未通过时检索不到" "$(jq '.data.list | length' /tmp/resp.json)" "0"

CODE=$(req POST /transcripts/$T1/submit "$IV")
check "提交审核" "$CODE" "200"; check "状态 submitted" "$(jq -r '.data.status' /tmp/resp.json)" "submitted"

# 通过前导出应被拒绝
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/transcripts/$T1/export" -H "Authorization: Bearer $AR")
check "未通过时导出被拒" "$CODE" "409"

CODE=$(req POST /transcripts/$T1/segments/$SEG1/confirm "$AR"); check "确认分段1" "$CODE" "200"
CODE=$(req POST /transcripts/$T1/segments/$SEG2/confirm "$AR"); check "确认分段2" "$CODE" "200"
CODE=$(req POST /transcripts/$T1/approve "$AR")
check "有未确认分段时不能通过" "$CODE" "409"
CODE=$(req POST /transcripts/$T1/segments/$SEG3/confirm "$AR"); check "确认分段3" "$CODE" "200"
CODE=$(req POST /transcripts/$T1/approve "$AR")
check "全部确认后通过" "$CODE" "200"; check "状态 approved" "$(jq -r '.data.status' /tmp/resp.json)" "approved"

req GET "/transcripts/search?q=老槐树&project_id=$P1" "$IV" >/dev/null
check "通过后检索命中" "$(jq '.data.list | length' /tmp/resp.json)" "1"
check "检索命中内容" "$(jq -r '.data.list[0].content' /tmp/resp.json)" "您还记得村里的老槐树吗？"
CODE=$(curl -s -o /tmp/export1.txt -w "%{http_code}" "$BASE/transcripts/$T1/export" -H "Authorization: Bearer $AR")
check "通过后导出" "$CODE" "200"
grep -q "老槐树" /tmp/export1.txt && ok "导出内容包含分段" || bad "导出内容缺失"

say "2. 审核退回（必须写明问题）与重提"
CODE=$(req POST /transcripts "$IV" "{\"recording_id\":$R1,\"project_id\":$P1}")
T2=$(jq '.data.id' /tmp/resp.json); V=$(jq '.data.version' /tmp/resp.json)
check "已通过内容修改生成新版本" "$CODE" "200"
check "新版本为 v2" "$V" "2"
check "v2 复制了 v1 分段" "$(jq '.data.segments | length' /tmp/resp.json)" "3"

CODE=$(req PUT /transcripts/$T2/segments "$IV" '{"segments":[
  {"start_second":0,"end_second":30,"speaker":"张老","content":"我出生在山东的一个小村庄，村东头有条河。"},
  {"start_second":30,"end_second":75,"speaker":"采访员","content":"您还记得村里的老槐树吗？"},
  {"start_second":75,"end_second":120,"speaker":"张老","content":"记得，树下是村里开会的地方。"}]}')
check "v2 修改分段" "$CODE" "200"
CODE=$(req POST /transcripts/$T2/submit "$IV"); check "v2 提交" "$CODE" "200"

CODE=$(req POST /transcripts/$T2/reject "$AR" '{}')
check "退回不写问题被拒(校验)" "$CODE" "400"
CODE=$(req POST /transcripts/$T2/reject "$AR" '{"reason":"  "}')
check "退回空白原因被拒" "$CODE" "400"
CODE=$(req POST /transcripts/$T2/reject "$AR" '{"reason":"第二段说话人标注有误，请核对录音"}')
check "写明问题后退回成功" "$CODE" "200"
check "状态 rejected" "$(jq -r '.data.status' /tmp/resp.json)" "rejected"
check "退回原因已记录" "$(jq -r '.data.review_comment' /tmp/resp.json)" "第二段说话人标注有误，请核对录音"

CODE=$(req PUT /transcripts/$T2/segments "$IV" '{"segments":[
  {"start_second":0,"end_second":30,"speaker":"张老","content":"我出生在山东的一个小村庄，村东头有条河。"},
  {"start_second":30,"end_second":75,"speaker":"采访员","content":"您还记得村口的老槐树吗？"},
  {"start_second":75,"end_second":120,"speaker":"张老","content":"记得，树下是村里开会的地方。"}]}')
check "退回后可重新编辑" "$CODE" "200"
check "编辑后回到 draft" "$(jq -r '.data.status' /tmp/resp.json)" "draft"
CODE=$(req POST /transcripts/$T2/submit "$IV"); check "重新提交" "$CODE" "200"
NSEG=$(jq '.data.segments | length' /tmp/resp.json)
for i in $(seq 0 $((NSEG-1))); do
  SID=$(jq ".data.segments[$i].id" /tmp/resp.json)
  req POST /transcripts/$T2/segments/$SID/confirm "$AR" >/dev/null
  req GET /transcripts/$T2 "$AR" >/dev/null
done
CODE=$(req POST /transcripts/$T2/approve "$AR"); check "重提后通过" "$CODE" "200"

req GET "/transcripts/search?q=村口&project_id=$P1" "$IV" >/dev/null
check "检索命中新版本内容" "$(jq '.data.list | length' /tmp/resp.json)" "1"
req GET "/transcripts/search?q=村东头&project_id=$P1" "$IV" >/dev/null
check "新版本独有内容可检索" "$(jq '.data.list | length' /tmp/resp.json)" "1"
CODE=$(curl -s -o /tmp/export_v1.txt -w "%{http_code}" "$BASE/transcripts/$T1/export" -H "Authorization: Bearer $AR")
check "旧版本 v1 仍可导出（保留旧版）" "$CODE" "200"
CODE=$(curl -s -o /tmp/export_v2.txt -w "%{http_code}" "$BASE/transcripts/$T2/export" -H "Authorization: Bearer $AR")
check "新版本 v2 可导出" "$CODE" "200"

say "3. 越权访问"
CODE=$(req POST /transcripts/$T2/approve "$IV"); check "采访员不能审核通过" "$CODE" "403"
CODE=$(req POST /transcripts/$T2/reject "$IV" '{"reason":"x"}'); check "采访员不能退回" "$CODE" "403"
CODE=$(req POST /transcripts/$T2/segments/$SEG1/confirm "$IV"); check "采访员不能确认分段" "$CODE" "403"
CODE=$(req POST /transcripts "$AR" "{\"recording_id\":$R4,\"project_id\":$P1}"); check "档案员不能创建草稿" "$CODE" "403"
CODE=$(req PUT /transcripts/$T2/segments "$AR" '{"segments":[]}'); check "档案员不能编辑分段" "$CODE" "403"
CODE=$(req POST /transcripts/$T2/submit "$AR"); check "档案员不能提交" "$CODE" "403"
CODE=$(req GET /transcripts/$T2 ""); check "未登录访问被拒" "$CODE" "401"

say "4. 跨项目写入防护"
CODE=$(req POST /transcripts "$IV" "{\"recording_id\":$R1,\"project_id\":$P2}")
check "录音与项目不匹配(跨项目)" "$CODE" "403"

say "5. 录音未就绪不能创建转写"
CODE=$(req POST /transcripts "$IV" "{\"recording_id\":$R3,\"project_id\":$P1}")
check "未就绪录音创建草稿被拒" "$CODE" "409"

say "6. 并发修改防护"
# 并发创建同一录音的草稿：一个成功一个冲突
(req POST /transcripts "$IV" "{\"recording_id\":$R4,\"project_id\":$P1}" > /tmp/cc1.txt; echo $(jq '.data.id // empty' /tmp/resp.json) > /tmp/cc1.id) &
(req POST /transcripts "$IV" "{\"recording_id\":$R4,\"project_id\":$P1}" > /tmp/cc2.txt) &
wait
C1=$(cat /tmp/cc1.txt); C2=$(cat /tmp/cc2.txt)
if { [ "$C1" == "200" ] && [ "$C2" == "409" ]; } || { [ "$C1" == "409" ] && [ "$C2" == "200" ]; }; then
  ok "并发创建草稿：一成功一冲突 ($C1/$C2)"
else
  bad "并发创建草稿：期望 200+409, 实际 $C1/$C2"
fi
T4=$(req GET "/transcripts?recording_id=$R4" "$IV" >/dev/null; jq '.data.list[0].id' /tmp/resp.json)
req PUT /transcripts/$T4/segments "$IV" '{"segments":[{"start_second":0,"end_second":60,"speaker":"张老","content":"并发测试分段。"}]}' >/dev/null
# 并发提交：一个成功一个冲突
req POST /transcripts/$T4/submit "$IV" > /tmp/cs1.txt &
req POST /transcripts/$T4/submit "$IV" > /tmp/cs2.txt &
wait
S1=$(cat /tmp/cs1.txt); S2=$(cat /tmp/cs2.txt)
if { [ "$S1" == "200" ] && [ "$S2" == "409" ]; } || { [ "$S1" == "409" ] && [ "$S2" == "200" ]; }; then
  ok "并发提交：一成功一冲突 ($S1/$S2)"
else
  bad "并发提交：期望 200+409, 实际 $S1/$S2"
fi
# 已提交后（模拟另一编辑窗口未刷新）继续保存应被拒
CODE=$(req PUT /transcripts/$T4/segments "$IV" '{"segments":[{"start_second":0,"end_second":10,"speaker":"x","content":"stale edit"}]}')
check "提交后陈旧编辑被拒" "$CODE" "409"
# 并发确认同一分段：均可或一个成功（幂等确认允许重复），此处验证不会出错
req GET /transcripts/$T4 "$AR" >/dev/null; SID=$(jq '.data.segments[0].id' /tmp/resp.json)
req POST /transcripts/$T4/segments/$SID/confirm "$AR" >/dev/null
CODE=$(req POST /transcripts/$T4/approve "$AR"); check "并发场景后审核通过" "$CODE" "200"

say "7. 项目归档后不能写入"
req PUT /projects/$P2/status "$IV" '{"status":"completed"}' >/dev/null
req PUT /projects/$P2/status "$IV" '{"status":"archived"}' >/dev/null
check "P2 已归档" "$(req GET /projects/$P2 "$IV" >/dev/null; jq -r '.data.status' /tmp/resp.json)" "archived"
CODE=$(req POST /transcripts "$IV" "{\"recording_id\":$R2,\"project_id\":$P2}")
check "归档项目创建草稿被拒" "$CODE" "409"

say "8. 采访员只能操作自己负责项目的转写"
req POST /auth/register "" '{"username":"iv2","password":"pass123456","display_name":"采访员二","role":"interviewer"}' >/dev/null
IV2=$(login iv2 pass123456)
R5=$(mkrec $P1 $Q1 1)
CODE=$(req POST /transcripts "$IV2" "{\"recording_id\":$R5,\"project_id\":$P1}")
check "他人项目录音建草稿被拒" "$CODE" "403"
CODE=$(req POST /transcripts "$IV" "{\"recording_id\":$R5,\"project_id\":$P1}")
check "项目负责人本人建草稿" "$CODE" "200"
T5=$(jq '.data.id' /tmp/resp.json)
CODE=$(req PUT /transcripts/$T5/segments "$IV2" '{"segments":[{"start_second":0,"end_second":10,"speaker":"x","content":"越权写入"}]}')
check "他人保存分段被拒" "$CODE" "403"
CODE=$(req POST /transcripts/$T5/submit "$IV2")
check "他人提交审核被拒" "$CODE" "403"
R6=$(mkrec $P1 $Q1 1)
CODE=$(req POST /transcripts "$AD" "{\"recording_id\":$R6,\"project_id\":$P1}")
check "管理员不受项目归属限制" "$CODE" "200"
T6=$(jq '.data.id' /tmp/resp.json)
CODE=$(req DELETE /recordings/$R6 "$IV")
check "删除录音" "$CODE" "200"
CODE=$(req GET /transcripts/$T6 "$AD")
check "录音删除后其转写不可读取" "$CODE" "404"

say "9. 分段时间校验（非零长度且不重叠）"
CODE=$(req PUT /transcripts/$T5/segments "$IV" '{"segments":[{"start_second":5,"end_second":5,"speaker":"张老","content":"零长度分段"}]}')
check "零长度分段被拒" "$CODE" "400"
CODE=$(req PUT /transcripts/$T5/segments "$IV" '{"segments":[{"start_second":10,"end_second":5,"speaker":"张老","content":"负长度分段"}]}')
check "负长度分段被拒" "$CODE" "400"
CODE=$(req PUT /transcripts/$T5/segments "$IV" '{"segments":[
  {"start_second":0,"end_second":50,"speaker":"张老","content":"第一段"},
  {"start_second":40,"end_second":60,"speaker":"张老","content":"与第一段重叠"}]}')
check "重叠分段被拒" "$CODE" "400"
CODE=$(req PUT /transcripts/$T5/segments "$IV" '{"segments":[
  {"start_second":0,"end_second":50,"speaker":"张老","content":"第一段"},
  {"start_second":50,"end_second":120,"speaker":"采访员","content":"首尾相接不重叠"}]}')
check "首尾相接分段允许" "$CODE" "200"

say "10. 项目移除后转写与分段不再保留"
req POST /projects "$IV" '{"title":"待删除项目","interviewee_name":"测试","birth_year":1960}' >/dev/null
P3=$(jq '.data.id' /tmp/resp.json)
req PUT /projects/$P3/status "$IV" '{"status":"in_progress"}' >/dev/null
req POST /projects/$P3/questions "$IV" '{"content":"级联删除问题"}' >/dev/null
Q3=$(jq '.data.id' /tmp/resp.json)
R7=$(mkrec $P3 $Q3 1)
CODE=$(req POST /transcripts "$IV" "{\"recording_id\":$R7,\"project_id\":$P3}")
T7=$(jq '.data.id' /tmp/resp.json)
req PUT /transcripts/$T7/segments "$IV" '{"segments":[{"start_second":0,"end_second":30,"speaker":"测试","content":"级联删除测试词"}]}' >/dev/null
req POST /transcripts/$T7/submit "$IV" >/dev/null
req GET /transcripts/$T7 "$IV" >/dev/null; SID7=$(jq '.data.segments[0].id' /tmp/resp.json)
req POST /transcripts/$T7/segments/$SID7/confirm "$AR" >/dev/null
req POST /transcripts/$T7/approve "$AR" >/dev/null
req GET "/transcripts/search?q=级联删除测试词" "$IV" >/dev/null
check "删除前检索命中" "$(jq '.data.list | length' /tmp/resp.json)" "1"
CODE=$(req DELETE /projects/$P3 "$IV")
check "删除项目" "$CODE" "200"
CODE=$(req GET /transcripts/$T7 "$IV")
check "项目删除后转写不可读取" "$CODE" "404"
req GET "/transcripts?recording_id=$R7" "$IV" >/dev/null
check "项目删除后版本列表为空" "$(jq '.data.list | length' /tmp/resp.json)" "0"
req GET "/transcripts/search?q=级联删除测试词" "$IV" >/dev/null
check "项目删除后检索不到分段" "$(jq '.data.list | length' /tmp/resp.json)" "0"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/transcripts/$T7/export" -H "Authorization: Bearer $AR")
check "项目删除后导出不可用" "$CODE" "404"

say "11. 转写查看权限（采访员仅见自己负责项目）"
CODE=$(req GET /transcripts/$T1 "$IV2")
check "他人查看转写详情被拒" "$CODE" "403"
CODE=$(req GET "/transcripts?recording_id=$R1" "$IV2")
check "他人按录音列版本被拒" "$CODE" "403"
CODE=$(req GET "/transcripts?project_id=$P1" "$IV2")
check "他人按项目列转写被拒" "$CODE" "403"
# iv2 自己负责的项目与转写
req POST /projects "$IV2" '{"title":"iv2的项目","interviewee_name":"王阿姨","birth_year":1945}' >/dev/null
P4=$(jq '.data.id' /tmp/resp.json)
req PUT /projects/$P4/status "$IV2" '{"status":"in_progress"}' >/dev/null
req POST /projects/$P4/questions "$IV2" '{"content":"说说您的工作经历"}' >/dev/null
Q4=$(jq '.data.id' /tmp/resp.json)
req POST /recordings "$IV2" "{\"project_id\":$P4,\"question_id\":$Q4,\"duration_seconds\":60}" >/dev/null
R8=$(jq '.data.id' /tmp/resp.json)
curl -s -o /dev/null -X POST "$BASE/recordings/$R8/audio" -H "Authorization: Bearer $IV2" -F "file=@/tmp/a.webm" -F "duration_seconds=60"
CODE=$(req POST /transcripts "$IV2" "{\"recording_id\":$R8,\"project_id\":$P4}")
T8=$(jq '.data.id' /tmp/resp.json)
check "iv2 自己项目建草稿" "$CODE" "200"
req GET /transcripts "$IV2" >/dev/null
check "iv2 列表包含自己转写" "$(jq "[.data.list[].id] | contains([$T8])" /tmp/resp.json)" "true"
check "iv2 列表不含他人转写" "$(jq "[.data.list[].id] | contains([$T1])" /tmp/resp.json)" "false"
req GET /transcripts "$IV" >/dev/null
check "iv1 列表不含 iv2 转写" "$(jq "[.data.list[].id] | contains([$T8])" /tmp/resp.json)" "false"
CODE=$(req GET /transcripts/$T1 "$IV")
check "项目负责人查看自己转写" "$CODE" "200"
CODE=$(req GET "/transcripts?recording_id=$R1" "$IV")
check "项目负责人按录音列版本" "$CODE" "200"
CODE=$(req GET /transcripts/$T1 "$AR")
check "档案员按职责查看详情" "$CODE" "200"
CODE=$(req GET "/transcripts?project_id=$P1" "$AR")
check "档案员按职责查看列表" "$CODE" "200"
CODE=$(req GET /transcripts/$T1 "$AD")
check "管理员查看详情" "$CODE" "200"
req GET "/transcripts/search?q=村口&project_id=$P1" "$IV2" >/dev/null
check "他人仍可检索已通过内容" "$(jq '.data.list | length' /tmp/resp.json)" "1"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/transcripts/$T2/export" -H "Authorization: Bearer $IV2")
check "他人仍可导出已通过版本" "$CODE" "200"

say "结果汇总"
echo -e "  \033[32m通过 $PASS\033[0m / \033[31m失败 $FAIL\033[0m"
[ $FAIL -eq 0 ] && echo "  全部验证通过 ✅" || { echo "  存在失败项 ❌"; exit 1; }
