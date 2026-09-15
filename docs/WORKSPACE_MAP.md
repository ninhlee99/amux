# Workspace map — client qua proxy

amux phục vụ **nhiều repo client**. Mỗi project có bản đồ riêng tại
`~/.am/workspaces/<project-name>/` — **không** dùng map repo amux khi code app khác.

---

## Init vs check/update (token)

| Lệnh | Phạm vi | Khi nào |
|------|---------|---------|
| `am map init` | **FULL** scan tree | Lần đầu / thiếu map |
| `am map update` | **FULL** regenerate cấu trúc | Đổi lớn kiến trúc (giữ `annotations.json`) |
| `am map recent` | **HẸP** git-changed ∪ touched | Check / quyết định learn |
| `am map touch` | ghi `focus.json` | Vừa mở 1 file/func |
| `am map graph <mod>` | **1 subnet** nơ-ron con | Khi đã biết module |
| `am map get` | **1 dòng** | Xem summary 1 func |
| `am map learn` | **1–N func** đã đọc sâu | Update map kiến thức |

**Init giữ nguyên (full).** Kiểm tra + enrich map **chỉ** recent/touched — không quét lại cả map/repo trong context AI.

```bash
cd /path/to/your-app
am map init                 # lần đầu ONLY
am map recent --needs-learn # shell/go — danh sách hẹp
am map touch --file pkg/auth/login.go --func Login
am map get --file pkg/auth/login.go --func Login
am map learn --file pkg/auth/login.go --func Login --summary "Issues JWT after bcrypt"
am map show
```

### Vòng học tối ưu token

1. `am map recent [--needs-learn]` (Go/git — 0 API token)  
2. Chỉ mở **source** các func trong list đó  
3. `am map learn …`  
4. **Cấm:** dump full `MODULES` kiểu bảng / full GRAPH mọi subnet cùng lúc  
5. Đọc GRAPH: **mesh + hubs**; subnet: `am map graph <module>`  
6. Batch: `echo '[...]' | am map learn --stdin`

Proxy lần đầu thiếu map → `EnsureMapIfMissing` = init (full). Agent sau đó chỉ `recent` + `learn`.

Nội dung workspace:

| File | Nội dung |
|------|----------|
| `AI_CODEBASE_MAP.md` | stack + bảng module (sinh lúc init/update) |
| **`GRAPH.md`** | **AI đọc cái này** — mesh module↔module + subnet func (nơ-ron) |
| `MODULES.md` | stub đếm file/func → trỏ GRAPH |
| `ai-locate.yaml` | tasks + **hubs** (không dump mọi func) |
| `annotations.json` | learn — sống qua `am map update` |
| `focus.json` | session touched (git∪touch) |
| `project_root.txt` | **abs** path máy — nguồn sự thật cho Resolve |
| `GENERATED.txt` | metadata local (root = abs) |
| `AGENTS.md` | pointer |

`docs/GRAPH.md` (+ stub `MODULES.md`, `MAP_GENERATED.txt`): **commit trong repo amux** (dùng máy khác không cần `am map update`).  
Project client khác: map **chỉ** `~/.am/workspaces/` — không bao giờ push vào git client.

---

## Agent dùng map thế nào

1. `am map show` hoặc `GET /_am/map?root=...` — chỉ paths  
2. Match keywords trong `ai-locate.yaml` (hoặc `am map recent`)  
3. Mở 1–3 source file — **không** dump MODULES  
4. Đọc sâu xong → `am map learn` + `am map touch`  
5. Lần sau: `am map recent --needs-learn` trước khi đọc lại  

---

## Hai loại bản đồ

| Loại | Khi nào | File |
|------|---------|------|
| **Tool amux** | Sửa gateway amux | `amux/docs/AI_CODEBASE_MAP.md` |
| **Client project** | App qua proxy | `~/.am/workspaces/<name>/` |

---

## Proxy

- `ProjectForRemoteAddr` → git root client
- Chưa có map → tự `GenerateMap` (init)
- Admin: `GET /_am/map?root=/path/to/app`
