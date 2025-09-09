package utils

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"math/big"
	"net"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CodeDogRun/go-framework/config"
)

// IsEmpty
// @Description: 判断any类型数据是否为空
func IsEmpty(val any) bool {
	if val == nil {
		return true
	}
	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !IsEmpty(v.Field(i).Interface()) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// FilterSymbol
// @Description: 过滤特殊字符
func FilterSymbol(src string) string {
	lower0 := []string{"ａ", "ｂ", "ｃ", "ｄ", "ｅ", "ｆ", "ｇ", "ｈ", "ｉ", "ｊ", "ｋ", "ｌ", "ｍ", "ｎ", "ｏ", "ｐ", "ｑ", "ｒ", "ｓ", "ｔ", "ｕ", "ｖ", "ｗ", "ｘ", "ｙ", "ｚ"}
	lower1 := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"}
	upper0 := []string{"Ａ", "Ｂ", "Ｃ", "Ｄ", "Ｅ", "Ｆ", "Ｇ", "Ｈ", "Ｉ", "Ｊ", "Ｋ", "Ｌ", "Ｍ", "Ｎ", "Ｏ", "Ｐ", "Ｑ", "Ｒ", "Ｓ", "Ｔ", "Ｕ", "Ｖ", "Ｗ", "Ｘ", "Ｙ", "Ｚ"}
	upper1 := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}
	number0 := []string{"１", "２", "３", "４", "５", "６", "７", "８", "９", "０"}
	number1 := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"}
	symbol0 := []string{"｀", "～", "！", "＠", "＃", "＄", "％", "＾", "＆", "＊", "（", "）", "＿", "＋", "＝", "－", "｛", "｝", "［", "］", "：", "＂", "；", "＇", "｜", "＜", "＞", "？", "，", "．", "／", "，", "。", "＼"}
	symbol1 := []string{"`", "~", "!", "@", "#", "$", "%", "^", "&", "*", "(", ")", "_", "+", "=", "-", "{", "}", "[", "]", ":", "\"", ";", "’", "|", "<", ">", "?", ",", ".", "/", "", "", ""}
	reg := regexp.MustCompile("[\U00010000-\U0010ffff]")
	src = reg.ReplaceAllString(src, "")
	src = strings.ToLower(src)

	src = strings.ReplaceAll(src, "\r", "")
	src = strings.ReplaceAll(src, "\n", "")
	src = strings.ReplaceAll(src, "\r\n", "")
	src = strings.ReplaceAll(src, "\n\r", "")
	src = strings.ReplaceAll(src, "\t", "")
	src = strings.ReplaceAll(src, "\t\n", "")
	src = strings.ReplaceAll(src, "\n\t", "")
	src = strings.ReplaceAll(src, "&amp;", "&")
	src = strings.ReplaceAll(src, "<0xa0>", "")

	for index, word := range lower0 {
		src = strings.ReplaceAll(src, word, lower1[index])
	}
	for index, word := range upper0 {
		src = strings.ReplaceAll(src, word, upper1[index])
	}
	for index, word := range number0 {
		src = strings.ReplaceAll(src, word, number1[index])
	}
	for index, word := range symbol0 {
		src = strings.ReplaceAll(src, word, symbol1[index])
	}
	src = strings.ReplaceAll(src, " ", "")
	return src
}

// InArrayString
// @Description: 判断字符串是否在数组内
func InArrayString(src string, target []string) bool {
	sort.Strings(target)
	index := sort.SearchStrings(target, src)
	if index < len(target) && target[index] == src {
		return true
	}
	return false
}

// InArrayInt64
// @Description: 判断int64是否在数组内
func InArrayInt64(src int64, target []int64) bool {
	m := make(map[int64]bool)
	for _, v := range target {
		m[v] = true
	}
	return m[src]
}

// InArrayUint64
// @Description: 判断uint64是否在数组内
func InArrayUint64(src uint64, target []uint64) bool {
	m := make(map[uint64]bool)
	for _, v := range target {
		m[v] = true
	}
	return m[src]
}

// TernaryIf
// @Description: 三元表达式
func TernaryIf[T any](condition bool, trueValue, falseValue T) T {
	if condition {
		return trueValue
	}
	return falseValue
}

// TernaryIfFunc
// @Description: 三元表达式-返回方法
func TernaryIfFunc[T any](condition bool, trueValue, falseValue func() T) T {
	if condition {
		return trueValue()
	}
	return falseValue()
}

// FileHash
// @Description: 计算文件哈希
func FileHash(filePath string, h hash.Hash) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buf := make([]byte, 4*1024*1024) // 4MB 缓冲区
	for {
		n, err := file.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// Md5EnCrypt
// @Description: MD5加密
func Md5EnCrypt(data string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}

// AesEnCrypt
// @Description: AES加密
func AesEnCrypt(plainBytes []byte) []byte {
	block, _ := aes.NewCipher([]byte(config.GetString("aes.key")))
	// 填充明文数据
	plainBytes = PKCS7Padding(plainBytes, aes.BlockSize)
	// 使用CBC模式加密
	mode := cipher.NewCBCEncrypter(block, []byte(config.GetString("aes.iv")))
	cipherText := make([]byte, len(plainBytes))
	mode.CryptBlocks(cipherText, plainBytes)
	return cipherText
}

// PKCS7Padding
// @Description: 添加PKCS7填充
func PKCS7Padding(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

// AesDeCrypt
// @Description: AES解密
func AesDeCrypt(content []byte) ([]byte, error) {
	if len(content) < aes.BlockSize {
		return nil, fmt.Errorf("content too short")
	}
	if len(content)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("content is not a multiple of the block size")
	}
	block, _ := aes.NewCipher([]byte(config.GetString("aes.key")))
	mode := cipher.NewCBCDecrypter(block, []byte(config.GetString("aes.iv")))
	var dst = make([]byte, len(content))
	mode.CryptBlocks(dst, content)
	length := len(dst)
	unPadding := int(dst[length-1])
	return dst[:(length - unPadding)], nil
}

// Random
// @Description: 随机生成字符串
func Random(l int, t int) string {
	str := "0123456789abcdefhiklmnorstuwxz"
	if t == 1 {
		str = "0123456789"
	} else if t == 2 {
		str = "0123456789abcdef"
	} else if t == 3 {
		str = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	} else if t == 4 {
		str = "abcdef"
	} else if t == 5 {
		str = "abcdefhiklmnrst"
	}
	buf := bytes.NewBufferString(str)
	bigInt := big.NewInt(int64(buf.Len()))

	result := ""
	for i := 0; i < l; i++ {
		randInt, _ := rand.Int(rand.Reader, bigInt)
		result += string(str[randInt.Int64()])
	}
	return result
}

// RandomImei
// @Description: 随机生成IMEI
func RandomImei(prefix string) string {
	imei := prefix + Random(6, 1)
	sum := 0
	for i := 0; i < len(imei); i++ {
		n, _ := strconv.Atoi(string(imei[i]))
		if i%2 == 1 {
			n *= 2
		}
		if n > 9 {
			n -= 9
		}
		sum += n
	}
	return fmt.Sprintf("%v%v", imei, (10-(sum%10))%10)
}

// RandomMeid
// @Description: 随机生成MEID
func RandomMeid(prefix string) string {
	return prefix + Random(6, 1)
}

// IsPrivateIp
// @Description: 判断是否为私有IP
func IsPrivateIp(ipString string) bool {
	if strings.HasPrefix(ipString, "127.0") {
		return true
	}

	parsedIP := net.ParseIP(ipString)
	if parsedIP == nil {
		// 如果IP无效，返回false
		return false
	}

	// 检查是否属于 10.0.0.0/8
	if parsedIP.IsPrivate() {
		return true
	}

	// 10.0.0.0/8
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	if cidr.Contains(parsedIP) {
		return true
	}

	// 172.16.0.0/12
	_, cidr, _ = net.ParseCIDR("172.16.0.0/12")
	if cidr.Contains(parsedIP) {
		return true
	}

	// 192.168.0.0/16
	_, cidr, _ = net.ParseCIDR("192.168.0.0/16")
	if cidr.Contains(parsedIP) {
		return true
	}

	return false
}

// GeneratePrivateIp
// @Description: 通过字符串生成固定内网IP
func GeneratePrivateIp(input string) string {
	hasher := fnv.New32a()
	hasher.Write([]byte(input))
	hashValue := hasher.Sum32()

	// 取哈希值的低 16 位，确保范围在 1-254
	ipPart1 := (hashValue >> 8) & 0xFF // 0-255
	ipPart2 := hashValue & 0xFF        // 0-255

	// 避免 .0 和 .255
	if ipPart1 == 0 {
		ipPart1 = 1
	}
	if ipPart1 == 255 {
		ipPart1 = 254
	}
	if ipPart2 == 0 {
		ipPart2 = 1
	}
	if ipPart2 == 255 {
		ipPart2 = 254
	}

	return fmt.Sprintf("192.168.%d.%d", ipPart1, ipPart2)
}
