package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"成功は 0":     {nil, 0},
		"差分は 1":     {errMismatch, 1},
		"差分を包んでも 1": {xerrors.Wrap(errMismatch, "x"), 1},
		"引数の誤りは 2":  {errUsage, 2},
		"検証できないは 2": {errPlanJSON, 2},
		"期待値の誤りは 2": {errExpectedMalformed, 2},
		"知らないエラーは 2（成功にも差分にも読まない）": {xerrors.New("x"), 2},
		"静的な資格情報は 2":   {errStaticCredentials, 2},
		"読むだけでない操作は 2": {errNotReadOnly, 2},
		"表の誤りは 2":      {errBadStep, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, exitCode(tc.err))
		})
	}
}

func Test_maskAccounts(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ in, want string }{
		"12桁の数字を伏せる":                   {"arn:aws:iam::123456789012:role/x", "arn:aws:iam::<account>:role/x"},
		"11桁は伏せない":                     {"12345678901", "12345678901"},
		"13桁は先頭の12桁を伏せて残りを残す":          {"1234567890123", "<account>3"},
		"複数あれば全部伏せる":                   {"111111111111 222222222222", "<account> <account>"},
		"commit の行は伏せない":               {"# commit: 000000000000abcdef0123456789abcdef01234\n", "# commit: 000000000000abcdef0123456789abcdef01234\n"},
		"16進の英字が後ろに続く12桁は伏せない":         {"x 000000000000abc", "x 000000000000abc"},
		"16進の英字が前に在る12桁は伏せない":          {"x abc000000000000", "x abc000000000000"},
		"commit の行の次の行は伏せる":            {"# commit: a\n111111111111\n", "# commit: a\n<account>\n"},
		"数字が無ければそのまま":                  {"# 結果: 不足 0 件", "# 結果: 不足 0 件"},
		"40桁の数字の sha の commit の行は伏せない": {"# commit: " + strings.Repeat("1", 40) + "\n", "# commit: " + strings.Repeat("1", 40) + "\n"},
		"行頭でない # commit: を含む行は伏せる":     {"x # commit: 111111111111\n", "x # commit: <account>\n"},
		"24桁は2つ伏せる":                    {strings.Repeat("1", 24), "<account><account>"},
		"12桁に数字、英字が続く並びは伏せて英字を残す":      {"1234567890123a", "<account>3a"},
		"空文字列": {"", ""},
		"日本語に隣り合う12桁は伏せる": {"件123456789012件", "件<account>件"},
		"末尾の改行を残す":        {"123456789012\n", "<account>\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, maskAccounts(tc.in))
		})
	}
}

func Test_parseArgs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("実 plan は header と入力を受ける", func(t *testing.T) {
			t.Parallel()

			o, err := parseArgs([]string{"-header-file", "h", "p.json"})
			require.NoError(t, err)
			assert.Equal(t, options{headerFile: "h", input: "p.json"}, o)
		})

		t.Run("--partial と二重ハイフンも受ける", func(t *testing.T) {
			t.Parallel()

			o, err := parseArgs([]string{"--partial", "--header-file", "h", "-"})
			require.NoError(t, err)
			assert.True(t, o.partial)
			assert.Equal(t, "-", o.input)
		})

		t.Run("mock は終了コードのファイルを受ける", func(t *testing.T) {
			t.Parallel()

			o, err := parseArgs([]string{"-mock", "-test-exit-file", "x", "t.jsonl"})
			require.NoError(t, err)
			assert.Equal(t, options{mock: true, testExitFile: "x", input: "t.jsonl"}, o)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, args := range map[string][]string{
			"位置引数が無い":             {"-header-file", "h"},
			"位置引数が2個":             {"-header-file", "h", "a", "b"},
			"知らないフラグ":             {"-x", "-header-file", "h", "p"},
			"実 plan に header が無い": {"p"},
			"実 plan に終了コードのファイル":  {"-header-file", "h", "-test-exit-file", "x", "p"},
			"mock と partial":      {"-mock", "-partial", "-test-exit-file", "x", "t"},
			"mock と header":       {"-mock", "-header-file", "h", "-test-exit-file", "x", "t"},
			"mock に終了コードのファイルが無い": {"-mock", "t"},
			"mock の終了コードのファイルが空":  {"-mock", "-test-exit-file", "", "t"},
			"期待値を差し替えるフラグは無い":     {"-expected", "e", "-header-file", "h", "p"},
			"フラグの値が欠けている":         {"-header-file"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := parseArgs(args)
				require.ErrorIs(t, err, errUsage)
			})
		}
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("実 plan", func(t *testing.T) {
		t.Parallel()

		setup := func(t *testing.T, expected, plan, header string) ([]string, deps) {
			t.Helper()

			dir := t.TempDir()

			return []string{"plan", "-header-file", write(t, dir, "h", header), write(t, dir, "p.json", plan)}, osDepsExpected("", write(t, dir, "e", expected))
		}

		t.Run("一致なら出力は commit・要約・結果の順で、エラーを返さない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, expectedBody, planJSON(matchingChanges()...), headerClean)
			require.NoError(t, run(args, &out, d))
			assert.Equal(t, "# commit: "+commitSHA+"\n"+
				"# 期待値: import 1 / add 1 / change 0 / destroy 0\n"+
				"# plan: import 1 / add 1 / change 0 / destroy 0\n"+
				"# 結果: 不足 0 件、余剰 0 件\n", out.String())
		})

		t.Run("未コミットの変更があれば commit の行がそれを述べる", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, expectedBody, planJSON(matchingChanges()...), headerDirty)
			require.NoError(t, run(args, &out, d))
			assert.Contains(t, out.String(), "# commit: "+commitSHA+" （作業ツリーに未コミットの変更あり）\n")
		})

		t.Run("差分があれば余剰と不足の行を出し、差分のエラーを返す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, "import b.y\nadd z.z\n", planJSON(matchingChanges()...), headerClean)
			require.ErrorIs(t, run(args, &out, d), errMismatch)
			assert.Contains(t, out.String(), "NG  余剰  add a.x\nNG  不足  add z.z\n# 結果: 不足 1 件、余剰 1 件\n")
			assert.NotContains(t, out.String(), "❌")
		})

		t.Run("標準入力の plan も読む", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, expectedBody, "", headerClean)
			args[len(args)-1] = "-"
			d.stdin = strings.NewReader(planJSON(matchingChanges()...))
			require.NoError(t, run(args, &out, d))
		})

		t.Run("検証できなければ理由を出してエラーを返し、結果の行は出さない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, expectedBody, "not json", headerClean)
			require.ErrorIs(t, run(args, &out, d), errPlanJSON)
			assert.Equal(t, errPlanJSON.Error()+"\n", out.String())
		})
	})

	t.Run("mock の plan", func(t *testing.T) {
		t.Parallel()

		setup := func(t *testing.T, expected, jsonl string) ([]string, deps) {
			t.Helper()

			dir := t.TempDir()

			return []string{"plan", "-mock", "-test-exit-file", write(t, dir, "x", "0\n"), write(t, dir, "t.jsonl", jsonl)}, osDepsExpected("", write(t, dir, "e", expected))
		}

		t.Run("一致なら commit の行を出さず、ラベルは mock の plan で、最後に ✅ の行を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, expectedBody, passingTestOutput(matchingChanges()...))
			require.NoError(t, run(args, &out, d))
			assert.Equal(t, "# 期待値: import 1 / add 1 / change 0 / destroy 0\n"+
				"# mock の plan: import 1 / add 1 / change 0 / destroy 0\n"+
				"# 結果: 不足 0 件、余剰 0 件\n"+
				"✅ check-plan.expected（在れば check-plan-bootstrapping.expected を足したもの）は identity root の mock の plan と一致します\n", out.String())
		})

		t.Run("差分があれば最後に ❌ の差分の行を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, "import b.y\n", passingTestOutput(matchingChanges()...))
			require.ErrorIs(t, run(args, &out, d), errMismatch)
			assert.Contains(t, out.String(), "NG  余剰  add a.x\n# 結果: 不足 0 件、余剰 1 件\n")
			assert.True(t, bytes.HasSuffix(out.Bytes(), []byte("❌ check-plan.expected（在れば check-plan-bootstrapping.expected を足したもの）が identity root の plan と一致しません（NG の行を期待値へ写す。docs/setup/bootstrap.md の「この文書の保守」）\n")))
		})

		t.Run("検証できなければ理由の後に ❌ の突き合わせられない行を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			args, d := setup(t, expectedBody, testOutputOf(runEvent(mockFile, mockRun, "fail")))
			require.ErrorIs(t, run(args, &out, d), errTestFailed)
			assert.Equal(t, errTestFailed.Error()+"\n"+
				"❌ check-plan.expected（在れば check-plan-bootstrapping.expected を足したもの）と identity root の plan を突き合わせられません（理由は上の行）\n", out.String())
		})

		t.Run("terraform test が落ちていれば出力の error の文を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			output := testOutputOf(errorEvent("Error: boom"), runEvent(mockFile, mockRun, "error"))
			args, d := setup(t, expectedBody, output)
			require.ErrorIs(t, run(args, &out, d), errTestFailed)
			assert.Contains(t, out.String(), "Error: boom\n"+errTestFailed.Error()+"\n")
		})
	})

	t.Run("verify は段の確認を回し、NG があれば差分のエラーを返す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := verifyFake(t, stage1Responses(), map[string]awsResult{
			stsAccountKey: okResult(testAccount), s3Key("get-bucket-versioning", testBucket): okResult(`{"Status":"Suspended"}`),
		})

		require.ErrorIs(t, run([]string{"verify", "-stage", "stage1", "-project", testProject, "-region", testRegion}, &out, verifyDeps(f, nil)), errMismatch)
		assert.Contains(t, out.String(), "NG  版管理が Enabled\n")
		assert.True(t, strings.HasSuffix(out.String(), "# 結果: 4 件のうち NG 1 件\n"))
	})

	t.Run("verify の output-policy の reference は、-reference-dir に渡した置き場から plan を読む", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := &fakeAWS{t: t, responses: map[string]awsResult{stsAccountKey: okResult(testAccount)}, handler: permissiveHandler}
		d := verifyDeps(f, map[string]string{"x/y/plans.txt": "x/y/runs/state-backend.jsonl\n", "x/y/inputs/state-backend.json": referencePlanJSON(t)})

		err := run([]string{"verify", "-stage", "output-policy", "-project", testProject, "-region", testRegion,
			"-policy-source", "reference", "-reference-dir", "x/y"}, &out, d)

		require.ErrorIs(t, err, errMismatch)
		assert.NotContains(t, out.String(), "plans.txt")
		assert.True(t, strings.HasSuffix(out.String(), "# 結果: 13 件のうち NG 7 件\n"), out.String())
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, args := range map[string][]string{
			"サブコマンドが無い":                   nil,
			"知らないサブコマンド":                  {"foo"},
			"plan の引数が足りない":               {"plan"},
			"verify の段が無い":                {"verify", "-project", "tabp", "-region", "r"},
			"plan-permissions の plan が無い": {"plan-permissions", "-project", "tabp", "-region", "r"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				var out bytes.Buffer

				require.ErrorIs(t, run(args, &out, osDeps("")), errUsage)
				assert.Contains(t, out.String(), "使い方:")
				assert.NotContains(t, out.String(), "❌")
			})
		}
	})
}

func Test_finish(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mock bool
		err  error
		want string
	}{
		"実 plan の一致は何も足さない":      {false, nil, ""},
		"実 plan の差分は何も足さない":      {false, errMismatch, ""},
		"実 plan の検証不能は理由だけを足す":   {false, errPlanJSON, errPlanJSON.Error() + "\n"},
		"mock の一致は ✅ を足す":        {true, nil, mockMatched + "\n"},
		"mock の差分は ❌ の差分の行を足す":   {true, errMismatch, mockMismatch + "\n"},
		"mock の検証不能は理由の後に ❌ を足す": {true, errPlanJSON, errPlanJSON.Error() + "\n" + mockUnverifiable + "\n"},
		"読むだけでない操作は理由を足す":        {false, errNotReadOnly, errNotReadOnly.Error() + "\n"},
		"表の誤りは理由を足す":             {false, errBadStep, errBadStep.Error() + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			got := finish(&out, tc.mock, tc.err)
			require.Equal(t, tc.err, got)
			assert.Equal(t, tc.want, out.String())
		})
	}
}

// executeSetup は、実 plan の引数と、期待値を差し込んだ依存を返す。
func executeSetup(t *testing.T, expected, plan string) ([]string, deps) {
	t.Helper()

	dir := t.TempDir()

	return []string{"plan", "-header-file", write(t, dir, "h", headerClean), write(t, dir, "p.json", plan)},
		osDepsExpected("", write(t, dir, "e", expected))
}

func Test_execute(t *testing.T) {
	t.Parallel()

	t.Run("引数の誤りは使い方を stdout へ書き、2 を返す", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		assert.Equal(t, 2, execute(nil, &stdout, osDeps("")))
		assert.Contains(t, stdout.String(), "使い方:")
	})

	t.Run("stdout へ書けなければ、判定が一致でも 2 を返す", func(t *testing.T) {
		t.Parallel()

		args, d := executeSetup(t, expectedBody, planJSON(matchingChanges()...))

		assert.Equal(t, 2, execute(args, errWriter{}, d))
	})

	t.Run("一致なら 0 を返す", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		args, d := executeSetup(t, expectedBody, planJSON(matchingChanges()...))

		assert.Equal(t, 0, execute(args, &stdout, d))
	})

	t.Run("差分なら 1 を返し、出力の12桁の数字は伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		args, d := executeSetup(t, "import b.y\nadd a.x\nadd 123456789012.x\n", planJSON(matchingChanges()...))

		assert.Equal(t, 1, execute(args, &stdout, d))
		assert.Contains(t, stdout.String(), "NG  不足  add <account>.x\n")
		assert.NotContains(t, stdout.String(), "123456789012")
	})

	t.Run("差分でも stdout へ書けなければ 2 を返す", func(t *testing.T) {
		t.Parallel()

		args, d := executeSetup(t, "import b.y\n", planJSON(matchingChanges()...))

		assert.Equal(t, 2, execute(args, errWriter{}, d))
	})

	t.Run("一部だけ書いて失敗しても 2 を返す", func(t *testing.T) {
		t.Parallel()

		args, d := executeSetup(t, expectedBody, planJSON(matchingChanges()...))

		assert.Equal(t, 2, execute(args, partialWriter{}, d))
	})

	accountArgs := []string{"account", "-project", testProject, "-region", testRegion}

	t.Run("account がすべて ok なら 0 を返す", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		assert.Equal(t, 0, execute(accountArgs, &stdout, accountDeps(newFake(t, nil))))
		assert.Contains(t, stdout.String(), "# 結果: 15 件のうち NG 0 件\n")
	})

	t.Run("account に NG があれば 1 を返し、理由の12桁の数字は伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		d := accountDeps(newFake(t, map[string]awsResult{
			jsonKey("iam", "get-user", "--user-name", testUser): failed("User arn:aws:iam::123456789012:user/alice is not authorized"),
		}))

		assert.Equal(t, 1, execute(accountArgs, &stdout, d))
		assert.Contains(t, stdout.String(), "NG  1-3 一時 IAM user の path が /（呼び出しが失敗: User arn:aws:iam::<account>:user/alice is not authorized）\n")
		assert.NotContains(t, stdout.String(), "123456789012")
	})

	t.Run("account が一時 IAM user でなければ理由を出して 2 を返し、ARN の12桁の数字は伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		d := accountDeps(newFake(t, map[string]awsResult{
			"sts get-caller-identity --query Arn --output text --region " + testRegion: okResult("arn:aws:iam::123456789012:root"),
		}))

		assert.Equal(t, 2, execute(accountArgs, &stdout, d))
		assert.Equal(t, "sts get-caller-identity の Arn: arn:aws:iam::<account>:root: 一時 IAM user の資格情報ではありません。make aws-login をやり直してください\n", stdout.String())
	})

	t.Run("account の出力は16進の英字に隣り合う12桁も伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		d := accountDeps(newFake(t, map[string]awsResult{
			jsonKey("iam", "get-user", "--user-name", testUser): failed("denied for cafe123456789012"),
		}))

		assert.Equal(t, 1, execute(accountArgs, &stdout, d))
		assert.Contains(t, stdout.String(), "denied for cafe<account>）\n")
	})

	for name, tc := range map[string]struct {
		d    func(deps) deps
		want string
	}{
		"AWS_PROFILE が空": {func(d deps) deps {
			d.getenv = func(string) string { return "" }

			return d
		}, errNoProfile.Error()},
		"aws を起動できない": {func(d deps) deps {
			d.aws = func(...string) awsResult { return awsResult{err: os.ErrNotExist} }

			return d
		}, errAWSStart.Error()},
	} {
		t.Run("account で"+name+"なら理由を出して 2 を返し、結果の行は出さない", func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer

			assert.Equal(t, 2, execute(accountArgs, &stdout, tc.d(accountDeps(newFake(t, nil)))))
			assert.Contains(t, stdout.String(), tc.want)
			assert.NotContains(t, stdout.String(), "# 結果:")
		})
	}

	verifyArgs := []string{"verify", "-stage", "stage1", "-project", testProject, "-region", testRegion}

	t.Run("verify がすべて ok なら 0 を返し、Bucket 名の12桁の数字は伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		f := verifyFake(t, stage1Responses(), map[string]awsResult{stsAccountKey: okResult(testAccount)})

		assert.Equal(t, 0, execute(verifyArgs, &stdout, verifyDeps(f, nil)))
		assert.Contains(t, stdout.String(), "# 結果: 4 件のうち NG 0 件\n")
		assert.NotContains(t, stdout.String(), "123456789012")
	})

	t.Run("verify に NG があれば 1 を返し、理由の12桁の数字は伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		f := verifyFake(t, stage1Responses(), map[string]awsResult{
			stsAccountKey: okResult(testAccount),
			s3Key("get-bucket-versioning", testBucket): failed("Access Denied for arn:aws:iam::123456789012:user/x"),
		})

		assert.Equal(t, 1, execute(verifyArgs, &stdout, verifyDeps(f, nil)))
		assert.Contains(t, stdout.String(), "NG  版管理が Enabled（呼び出しが失敗: Access Denied for arn:aws:iam::<account>:user/x）\n")
	})

	t.Run("verify でアカウント ID を取れなければ理由を出して 2 を返し、結果の行は出さない", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		f := verifyFake(t, nil, map[string]awsResult{stsAccountKey: okResult("None")})

		assert.Equal(t, 2, execute(verifyArgs, &stdout, verifyDeps(f, nil)))
		assert.Contains(t, stdout.String(), errNoAccount.Error())
		assert.NotContains(t, stdout.String(), "# 結果:")
	})

	t.Run("verify の出力は16進の英字に隣り合う12桁も伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		f := verifyFake(t, stage1Responses(), map[string]awsResult{
			stsAccountKey: okResult(testAccount),
			s3Key("get-bucket-versioning", testBucket): failed("denied for cafe123456789012"),
		})

		assert.Equal(t, 1, execute(verifyArgs, &stdout, verifyDeps(f, nil)))
		assert.Contains(t, stdout.String(), "denied for cafe<account>）\n")
	})

	t.Run("verify がすべて ok でも stdout へ書けなければ 2 を返す", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, stage1Responses(), map[string]awsResult{stsAccountKey: okResult(testAccount)})

		assert.Equal(t, 2, execute(verifyArgs, errWriter{}, verifyDeps(f, nil)))
	})

	planPermArgs := []string{"plan-permissions", "-plan", "tmp/123456789012/plan.json", "-project", testProject, "-region", testRegion}

	t.Run("plan-permissions で plan を読めなければ理由を出して 2 を返し、12桁の数字は伏せる", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		f := verifyFake(t, nil, nil)

		assert.Equal(t, 2, execute(planPermArgs, &stdout, verifyDeps(f, nil)))
		assert.Equal(t, "tmp/<account>/plan.json: "+errPlanPermInput.Error()+"\n", stdout.String())
		assert.Empty(t, f.calls)
	})

	for name, args := range map[string][]string{"account": accountArgs, "verify": verifyArgs, "plan-permissions": planPermArgs} {
		t.Run(name+" は静的な資格情報が環境に在れば理由を出して 2 を返す", func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer

			f := verifyFake(t, nil, nil)
			d := verifyDeps(f, nil)
			d.getenv = func(k string) string { return map[string]string{"AWS_PROFILE": "p", "AWS_CONFIG_FILE": "/x"}[k] }

			assert.Equal(t, 2, execute(args, &stdout, d))
			assert.Equal(t, "AWS_CONFIG_FILE: "+errStaticCredentials.Error()+"\n", stdout.String())
			assert.Empty(t, f.calls)
		})
	}

	t.Run("verify の引数の誤りは 2 を返す", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		assert.Equal(t, 2, execute([]string{"verify", "-stage", "x"}, &stdout, verifyDeps(verifyFake(t, nil, nil), nil)))
		assert.Contains(t, stdout.String(), "使い方:")
	})

	t.Run("account がすべて ok でも stdout へ書けなければ 2 を返す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 2, execute(accountArgs, errWriter{}, accountDeps(newFake(t, nil))))
	})

	t.Run("account の引数の誤りは 2 を返す", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		assert.Equal(t, 2, execute([]string{"account"}, &stdout, accountDeps(newFake(t, nil))))
		assert.Contains(t, stdout.String(), "使い方:")
	})
}

func Test_maskAllAccounts(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ in, want string }{
		"16進の英字に隣り合っても伏せる": {"cafe123456789012ab\n", "cafe<account>ab\n"},
		"commit の行も伏せる":    {"# commit: 123456789012\n", "# commit: <account>\n"},
		"11桁は伏せない":         {"12345678901\n", "12345678901\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, maskAllAccounts(tc.in))
		})
	}
}

func Test_maskLine(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ in, want string }{
		"12桁を伏せる":         {"a 123456789012 b", "a <account> b"},
		"英字に隣り合う12桁は残す":   {"a123456789012", "a123456789012"},
		"伏せる並びと残す並びが混在する": {"123456789012 x 123456789012f", "<account> x 123456789012f"},
		"数字が無ければそのまま":     {"abc", "abc"},
		"末尾の改行を残す":        {"123456789012\n", "<account>\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, maskLine(tc.in))
		})
	}
}

func Test_isHexLetterAt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		s    string
		i    int
		want bool
	}{
		"小文字の a-f":  {"a", 0, true},
		"大文字の A-F":  {"F", 0, true},
		"a-f の外の英字": {"g", 0, false},
		"f の境界":     {"f", 0, true},
		"A の境界":     {"A", 0, true},
		"G の境界":     {"G", 0, false},
		"a の直前の記号":  {"`", 0, false},
		"A の直前の記号":  {"@", 0, false},
		"数字":        {"1", 0, false},
		"範囲の前":      {"a", -1, false},
		"範囲の後":      {"a", 1, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, isHexLetterAt(tc.s, tc.i))
		})
	}
}
