package structured

// MinimalValidClarifiedRequirementJSON is a smallest payload that passes
// ParseClarifiedRequirement. Used by tests across packages via the mcp reexport
// or by writing equivalent JSON inline.
const MinimalValidClarifiedRequirementJSON = `{
  "title": "登录",
  "summary": "用户可用邮箱验证码登录",
  "background": "需要安全登录入口",
  "work_kind": "feature",
  "goals": ["完成邮箱验证码登录"],
  "in_scope": ["邮箱验证码登录"],
  "out_of_scope": ["第三方 OAuth"],
  "functional_requirements": [
    {
      "title": "验证码登录",
      "detail": "用户输入邮箱与验证码完成登录",
      "priority": "must",
      "acceptance_criteria": ["验证码 5 分钟内有效"]
    }
  ],
  "assumptions": ["用户已有邮箱"],
  "dependencies": ["邮件发送服务可用"],
  "constraints": ["仅邮箱登录"]
}`
