# Treeport

[English](README.md) · [프로필](docs/profiles.md) · [JSON/API 계약](docs/contract.md) · [실행 기반 비교](docs/comparison.md)

**맥에서 만든 ZIP을 윈도우에서 풀기 전에, 한글 파일명이 깨지는지 먼저 확인하세요.** 맥은 한글 파일명을 자모가 분리된 형태(NFD)로 저장하는 경우가 많아서, 그 이름이 그대로 윈도우로 가면 탐색기에서 `ㅎㅏㄴㄱㅡㄹ.txt`처럼 보이고 키보드로 친 `한글.txt`와 다른 파일이 됩니다. Treeport는 이런 이름을 `nfd_name` 경고로 알려 줍니다. 기존 도구들은 한글 NFC/NFD 충돌을 그냥 통과시킵니다([실행 기반 비교](docs/comparison.md)).

ZIP, 폴더, Git 저장소, JSONL 목록의 파일명을 읽고, 대상 OS에서 충돌하거나 쓸 수 없는 이름을 찾습니다. 대소문자만 다른 `README.md`와 `Readme.md`, 윈도우 예약어 `aux.c`, 끝에 붙은 점, 루트까지 합친 경로 길이 초과 같은 것들입니다. 원본 파일을 바꾸거나 압축을 풀지 않습니다.

```sh
treeport zip --profile windows --root 'C:\Users\me\Downloads' 보고서.zip
treeport git --profile windows --root 'C:\src\project'   # 윈도우에서 clone하면 깨지는 이름
```

## 빠른 시작

[Releases](https://github.com/rad1092/treeport/releases)에서 운영체제에 맞는 실행 파일과 체크섬을 받거나 Go 1.25 이상으로 설치합니다.

```sh
go install github.com/rad1092/treeport/cmd/treeport@v0.2.0
treeport scan --profile windows --root 'C:\export\release' ./dist
treeport zip --profile windows --root 'C:\export\release' --json release.zip
```

옵션은 입력 경로보다 앞에 둡니다. `--root`는 원본 폴더가 아니라 **향후 목적지 경로**입니다. Windows 프로필에는 드라이브 또는 UNC 절대 경로를 지정해야 합니다. 현재 컴퓨터에 그 경로가 있을 필요는 없습니다.

현재 파일시스템에 동시에 저장할 수 없는 이름은 JSONL로 검사할 수 있습니다.

```sh
printf '%s\n' '{"path":"CAFÉ/a?b.txt"}' '{"path":"cafe\u0301/a*b.txt"}' |
  treeport manifest --profile export-fold --json -
```

`export-fold`는 NFC 정규화 → Unicode case fold → Windows 금지문자 `_` 치환 → 끝의 점/공백 제거 → UTF-16 길이 절단 순서의 명시적 변환 모델입니다. 위 두 경로는 `café/a_b.txt`로 합쳐집니다. Treeport는 실제 이름을 바꾸지 않고 충돌과 그 원인만 보고합니다.

| 종료 코드 | 의미 |
| --- | --- |
| `0` | 선택한 모델에서 `known-compatible`, 또는 `unknown`이지만 문제없을 가능성이 높음 |
| `1` | 확정적인 `incompatible` 문제, 또는 `unknown`이지만 충돌할 가능성이 높음 |
| `2` | 잘못된 입력, 읽기 오류, 한도 초과, 취소 또는 실행 오류 |
| `3` | `unknown`이고 어느 쪽인지 판단할 수 없음(심볼릭 링크, 잘못된 UTF-8, 보고 한도) |

한글처럼 비 ASCII 이름은 윈도우·macOS의 정확한 비교 테이블을 알 수 없어 `unknown`이 되지만, 다른 이름과 겹치지 않으면 "문제없을 가능성이 높음" 힌트와 함께 0으로 끝납니다. 모든 `unknown`을 실패로 보려면 `--strict`를 붙이세요. JSON의 `complete`도 확인하세요. 보고량 한도에 걸리면 불완전함을 표시합니다. [CI/pre-commit 예제](docs/integrations.md)를 제공합니다.

## 모델을 명시하세요

`posix`, `windows`, `macos`, `export-fold`를 지원합니다. OS 이름은 제한된 검사 모델을 뜻합니다. 파일시스템, 마운트 옵션, API에 따라 실제 동작은 달라집니다. Windows/macOS의 비 ASCII 이름은 정확한 대상 비교 테이블을 알 수 없어 후보 충돌과 `unknown`을 보고합니다. ASCII만 허용하라는 뜻이 아니라, 판정의 한계를 드러내는 것입니다.

원본 이름은 JSON의 `base64`에 바이트 그대로 보존합니다. `display`는 사람이 읽기 위한 표현입니다. 잘못된 UTF-8 바이트는 manifest의 `path_base64`로 전달합니다.

Go 프로그램은 [`Check`](docs/contract.md)를 호출해 같은 결과를 얻습니다. [실행 가능한 예제](examples/embed/main.go)와 [기존 도구의 고정 커밋 실행 결과](docs/comparison.md)를 참고하세요.

파일 내용, 실제 목적지 파일, 권한, 링크 대상, 검사 중 경로 변경은 보장 범위가 아닙니다. 모든 환경에서의 이식성이나 ZIP 보안 검증을 보장하지 않습니다. 자세한 내용은 [한계](docs/limitations.md)와 [검증 범위](docs/verification.md)에 정리했습니다. [MIT 라이선스](LICENSE).

소스와 바이너리 배포에는 [제3자 라이선스 및 고정된 출처](third_party/README.md)를 함께 제공합니다.
