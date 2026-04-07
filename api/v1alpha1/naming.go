package v1alpha1

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"unicode"
)

// Kubernetes 对象名称长度限制常量
const (
	// CronJob 名称最大长度（DNS 标签标准为 63，这里预留一些空间给哈希和连字符）
	maxCronJobNameLength = 52
	// Job 名称最大长度（标准 DNS 标签长度）
	maxJobNameLength = 63
	// 标签值最大长度（标准 DNS 标签长度）
	maxLabelValueLength = 63
)

// BuildCronJobName 根据提供的部分构建一个合法的 CronJob 名称
// 内部调用 buildDNSLabelName，并传入 CronJob 允许的最大长度
func BuildCronJobName(parts ...string) string {
	return buildDNSLabelName(maxCronJobNameLength, parts...)
}

// BuildJobName 根据提供的部分构建一个合法的 Job 名称
// 内部调用 buildDNSLabelName，并传入 Job 允许的最大长度
func BuildJobName(parts ...string) string {
	return buildDNSLabelName(maxJobNameLength, parts...)
}

// BuildLabelValue 根据提供的部分构建一个合法的 Kubernetes 标签值
// 内部调用 buildDNSLabelName，并传入标签值允许的最大长度
func BuildLabelValue(parts ...string) string {
	return buildDNSLabelName(maxLabelValueLength, parts...)
}

// sanitizeName 将原始字符串转换为符合 DNS 标签规范的字符串
// 规则：
//   - 转为小写并去除首尾空格
//   - 仅保留小写字母、数字，其他字符（包括下划线、点、斜杠、空格等）均转换为连字符 '-'
//   - 连续多个非法字符只产生一个连字符
//   - 结果不能以连字符开头或结尾
//   - 若结果为空则返回默认值 "dp"
func sanitizeName(value string) string {
	// 去除首尾空格并转为小写
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "dp"
	}

	var builder strings.Builder
	lastHyphen := false // 标记上一个写入的字符是否为连字符，用于压缩连续分隔符
	for _, r := range value {
		switch {
		// 合法字符：小写字母或数字 -> 直接保留
		case unicode.IsLower(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastHyphen = false
		// 非法字符（分隔符类）：连字符、下划线、点、斜杠、空格等 -> 替换为连字符
		case r == '-' || r == '_' || r == '.' || r == '/' || unicode.IsSpace(r):
			if !lastHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				lastHyphen = true
			}
		// 其他任何字符（如标点、特殊符号）也视为分隔符
		default:
			if !lastHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}

	// 去除首尾可能残留的连字符
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "dp"
	}
	return result
}

// buildDNSLabelName 核心构建函数：将多个部分用连字符连接，并确保总长度不超过 maxLength
// 如果超过长度，则使用短哈希（8位）替换名称末尾，保留前缀部分
// 参数：
//   - maxLength: 允许的最大长度
//   - parts: 名称的组成部分（例如 namespace, name, kind 等）
func buildDNSLabelName(maxLength int, parts ...string) string {
	// 1. 对每个部分分别进行净化
	sanitized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeName(part)
		if part != "" {
			sanitized = append(sanitized, part)
		}
	}
	// 如果所有部分净化后都为空，则使用默认值
	if len(sanitized) == 0 {
		sanitized = []string{"dp"}
	}

	// 2. 用连字符连接得到完整名称
	name := strings.Join(sanitized, "-")
	// 3. 如果未超过长度限制，直接返回
	if len(name) <= maxLength {
		return name
	}

	// 4. 超过长度：计算短哈希（取 SHA1 的前8位十六进制）
	hash := shortHash(name)
	// 保留前缀的长度 = 最大长度 - 哈希长度 - 1（连字符）
	prefixLength := maxLength - len(hash) - 1
	if prefixLength < 1 {
		// 极端情况：最大长度太小，连一个字符加哈希都放不下
		if maxLength <= len(hash) {
			// 直接截取哈希的前 maxLength 个字符
			return hash[:maxLength]
		}
		// 否则返回哈希的适当前缀（保留一位连字符？实际上不会走到这里，但防御性处理）
		return hash[:maxLength-1]
	}

	// 5. 截取前缀，并去除结尾可能残留的连字符
	prefix := strings.Trim(name[:prefixLength], "-")
	if prefix == "" {
		prefix = "dp"
	}

	// 6. 组合前缀 + 连字符 + 哈希
	name = prefix + "-" + hash
	// 7. 最终安全截断（防止由于 Trim 后长度仍超出）
	if len(name) <= maxLength {
		return name
	}
	return strings.Trim(name[:maxLength], "-")
}

// shortHash 计算字符串的短哈希（8位十六进制）
// 使用 SHA1 算法，取前 8 个字符，足够在有限长度内保持唯一性
func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}
