package common

import (
	"fmt"
	"maps"
	"sync"
)

var topupGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}
var topupGroupRatioMutex sync.RWMutex

func TopupGroupRatio2JSONString() string {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	jsonBytes, err := Marshal(topupGroupRatio)
	if err != nil {
		SysError("error marshalling topup group ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func parseTopupGroupRatios(jsonStr string) (map[string]float64, error) {
	var ratios map[string]float64
	if err := UnmarshalJsonStr(jsonStr, &ratios); err != nil {
		return nil, err
	}
	for group, ratio := range ratios {
		if ratio < 0 {
			return nil, fmt.Errorf("top-up ratio for group %q cannot be negative", group)
		}
	}
	return ratios, nil
}

func ValidateTopupGroupRatioJSON(jsonStr string) error {
	_, err := parseTopupGroupRatios(jsonStr)
	return err
}

func UpdateTopupGroupRatioByJSONString(jsonStr string) error {
	ratios, err := parseTopupGroupRatios(jsonStr)
	if err != nil {
		return err
	}

	topupGroupRatioMutex.Lock()
	topupGroupRatio = ratios
	topupGroupRatioMutex.Unlock()
	return nil
}

func GetTopupGroupRatio(name string) float64 {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	ratio, ok := topupGroupRatio[name]
	if !ok {
		SysError("topup group ratio not found: " + name)
		return 1
	}
	return ratio
}

func GetTopupGroupRatioCopy() map[string]float64 {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()

	copyGroupRatio := make(map[string]float64, len(topupGroupRatio))
	maps.Copy(copyGroupRatio, topupGroupRatio)
	return copyGroupRatio
}
