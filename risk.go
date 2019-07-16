package main

import (
	"fmt"
	"github.com/thoas/go-funk"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

var predefinedGroupName = []string{
	"sector",
	"industry",
	"subindustry",
	"market",
	"type",
	"currency",
	"acc",
}

const (
	GROUP_SECTOR      = 0
	GROUP_INDUSTRY    = 1
	GROUP_SUBINDUSTRY = 2
	GROUP_MARKET      = 3
	GROUP_TYPE        = 4
	GROUP_CURRENCY    = 5
	GROUP_ACC         = 6
)

type WindowDef struct {
	Seconds int
	Type    string
}

type NameExpression struct {
	Name string
	E    *Expression
}

type RiskParamDef struct {
	Parent     *RiskDef
	Name       string
	Formula    *Expression
	UpperBound []float64
	LowerBound []float64
	TradeStop  bool
	Window     WindowDef
	Variables  []NameExpression
	Graph      bool
	History    map[string][][2]float64 // only if Graph = true
}

type RiskDef struct {
	Path        string // python module path
	Name        string
	Groups      []interface{}
	GroupNames  []string
	Params      []*RiskParamDef
	DisplayName string
	Filter      *Expression
}

func split(s string, pattern string) []string {
	res := strings.FieldsFunc(s, func(r rune) bool {
		return strings.Index(pattern, string(r)) >= 0
	})
	var res2 []string
	for _, s := range res {
		res2 = append(res2, strings.Trim(s, " \t\r"))
	}
	return res2
}

func newRiskParamDef(s *IniSection, parent *RiskDef) (r *RiskParamDef, eres error) {
	f := s.ValueMap["formula"]
	r = &RiskParamDef{
		Parent: parent,
		Name:   s.Name,
	}
	var params map[string]interface{}
	variables := s.SectionMap["var"]
	if variables != nil {
		params = make(map[string]interface{}, 60)
		for _, nameExpr := range variables.Values {
			res, err := ParseExpr(nameExpr[2], nameExpr[1], "variable", params, nil, parent.Path)
			if err != nil {
				eres = err
				return
			}
			r.Variables = append(r.Variables, NameExpression{nameExpr[0], res})
			params[nameExpr[0]] = 0.0
		}
	}
	if f[0] != "" {
		res, err := ParseExpr(f[1], f[0], "formula", params, nil, parent.Path)
		if err != nil {
			eres = err
			return
		}
		r.Formula = res
	}
	w := split(s.ValueMap["window"][0], ",")
	if len(w) > 0 {
		if v, err := strconv.Atoi(w[0]); err == nil {
			r.Window.Seconds = v
		}
	}
	if len(w) > 1 {
		r.Window.Type = w[1]
	}
	strs := split(s.ValueMap["upper_bound"][0], ",")
	for _, str := range strs {
		v := math.NaN()
		if v2, err := strconv.ParseFloat(str, 64); err == nil {
			v = v2
		}
		r.UpperBound = append(r.UpperBound, v)
	}
	strs = split(s.ValueMap["lower_bound"][0], ",")
	for _, str := range strs {
		v := math.NaN()
		if v2, err := strconv.ParseFloat(str, 64); err == nil {
			v = v2
		}
		r.LowerBound = append(r.LowerBound, v)
	}
	str := s.ValueMap["trade_stop"][0]
	if str != "" {
		if v, err := strconv.ParseBool(str); err == nil {
			r.TradeStop = v
		}
	}
	str = strings.ToLower(s.ValueMap["graph"][0])
	if str == "true" || str == "y" || str == "yes" || str == "1" {
		if r.Formula.A == "" {
			log.Print("Graph only allowable for aggregate formula")
		} else {
			r.Graph = true
			r.History = make(map[string][][2]float64)
		}
	}
	return
}

func newRiskDef(s *IniSection, path string) (r *RiskDef, eres error) {
	r = &RiskDef{
		Path:        path,
		Name:        s.Name,
		GroupNames:  split(s.ValueMap["group_name"][0], ","),
		DisplayName: s.ValueMap["name"][0],
	}
	if r.DisplayName == "" {
		r.DisplayName = r.Name
	}
	tmp := s.ValueMap["group"]
	groups := split(tmp[0], ",")
	for i, g := range groups {
		ig := funk.IndexOf(predefinedGroupName, g)
		if ig < 0 {
			if g == "*" {
				g = "true"
			}
			res, err := ParseExpr(tmp[1], g, "group", nil, true, path)
			if err != nil {
				eres = err
				return
			}
			r.Groups = append(r.Groups, res)
		} else {
			r.Groups = append(r.Groups, ig)
		}
		if i >= len(r.GroupNames) {
			r.GroupNames = append(r.GroupNames, g)
		}
	}
	f := s.ValueMap["f"]
	if f[0] != "" {
		res, err := ParseExpr(f[1], f[0], "filter", nil, true, r.Path)
		if err != nil {
			eres = err
			return
		}
		r.Filter = res
	}
	for _, p := range s.Sections {
		if p.Name == "var" {
			continue
		}
		rp, err := newRiskParamDef(p, r)
		if err != nil {
			eres = err
			return
		}
		r.Params = append(r.Params, rp)
	}
	rp, err := newRiskParamDef(s, r)
	if err != nil {
		eres = err
		return
	}
	if rp.Formula != nil {
		r.Params = append([]*RiskParamDef{rp}, r.Params...)
	}
	return
}

func (self *RiskDef) Run(positions []*Position, portfolioName string, userId int) interface{} {
	tradeStops := make(map[int]string)
	grouped := make(map[string][]*Position)
	var gnames []string // for making order stable when showing on gui
	igroupMap := make(map[string]int)
	if len(self.Groups) > 0 {
		for igroup, expr := range self.Groups {
			var subGroupNames []string
			e, eok := expr.(*Expression)
			for _, p := range positions {
				if self.Filter != nil {
					v, _ := Evaluate(self.Filter, p)
					if v2, ok2 := v.(bool); ok2 {
						if !v2 {
							continue
						}
					}
				}
				tmp := ""
				if eok {
					v, _ := Evaluate(e, p)
					if v2, ok2 := v.(bool); ok2 {
						if v2 {
							tmp = self.GroupNames[igroup]
						}
					}
				} else {
					switch expr {
					case GROUP_SECTOR:
						tmp = p.Security.Sector
					case GROUP_INDUSTRY:
						tmp = p.Security.Industry
					case GROUP_SUBINDUSTRY:
						tmp = p.Security.SubIndustry
					case GROUP_MARKET:
						tmp = p.Security.Market
					case GROUP_TYPE:
						tmp = p.Security.Type
					case GROUP_CURRENCY:
						tmp = p.Security.Currency
					case GROUP_ACC:
						tmp = AccNames[p.Acc]
					}
				}
				if tmp != "" {
					if grouped[tmp] == nil {
						subGroupNames = append(subGroupNames, tmp)
						igroupMap[tmp] = igroup
					}
					grouped[tmp] = append(grouped[tmp], p)
				}
			}
			if len(self.GroupNames) > igroup {
				expr = self.GroupNames[igroup]
			}
			if subGroupNames != nil {
				sort.Strings(subGroupNames)
				for _, name := range subGroupNames {
					gnames = append(gnames, name)
				}
			}
		}
	} else {
		grouped[""] = positions
		gnames = append(gnames, "")
	}
	rpt := make(map[string]interface{})
	for _, rp := range self.Params {
		var out []interface{}
		for _, gname := range gnames {
			positions := grouped[gname]
			if len(positions) > 0 {
				value := rp.Run(gname, positions)
				igroup := igroupMap[gname]
				lowerBound := math.NaN()
				if len(rp.LowerBound) > 0 {
					if igroup >= len(rp.LowerBound) {
						lowerBound = rp.LowerBound[len(rp.LowerBound)-1]
					} else {
						lowerBound = rp.LowerBound[igroup]
					}
				}
				upperBound := math.NaN()
				if len(rp.UpperBound) > 0 {
					if igroup >= len(rp.UpperBound) {
						upperBound = rp.UpperBound[len(rp.UpperBound)-1]
					} else {
						upperBound = rp.UpperBound[igroup]
					}
				}
				if floatValue, ok := value.(float64); ok {
					var breach []interface{}
					if floatValue < lowerBound {
						breach = append(breach, -1)
					} else if floatValue > upperBound {
						breach = append(breach, 1)
					}
					if breach != nil {
						if rp.TradeStop {
							reason := fmt.Sprintf("OpenRisk: %d '%s' '%s' '%s' '%s' value %f out of range [%f, %f]", userId, portfolioName, self.Name, rp.Name, gname, floatValue, lowerBound, upperBound)
							for _, pos := range positions {
								tradeStops[pos.Acc] = reason
							}
							breach = append(breach, true)
						}
						out = append(out, []interface{}{gname, value, breach})
						continue
					}
				} else if array, ok := value.([][2]interface{}); ok {
					var newArray []interface{}
					for _, item := range array {
						if floatValue, ok := item[1].(float64); ok {
							var breach []interface{}
							if floatValue < lowerBound {
								breach = append(breach, -1)
							} else if floatValue > upperBound {
								breach = append(breach, 1)
							}
							// non-aggregate not support trade stop yet
							if breach != nil {
								newArray = append(newArray, []interface{}{item[0], item[1], breach})
								continue
							}
						}
						newArray = append(newArray, item)
					}
					value = newArray
				}
				out = append(out, []interface{}{gname, value})
			}
		}
		for acc, reason := range tradeStops {
			fmt.Print(acc, reason)
			Request(Array{"admin", "sub accounts", "disable", acc, reason})
		}
		if len(out) > 0 {
			if len(self.Params) == 1 {
				return out
			} else {
				rpt[rp.Name] = out
			}
		}
	}
	if len(rpt) > 0 {
		return rpt
	}
	return nil
}

func length(nums []float64) float64 {
	return float64(len(nums))
}

func sum(nums []float64) float64 {
	res := 0.
	for _, tmp := range nums {
		res += tmp
	}
	return res
}

func mean(nums []float64) float64 {
	if len(nums) == 0 {
		return math.NaN()
	}
	return sum(nums) / float64(len(nums))
}

func std(nums []float64) float64 {
	// std = sqrt(mean(abs(x - x.mean())**2))
	if len(nums) == 0 {
		return math.NaN()
	}
	mn := mean(nums)
	sd := 0.
	for _, tmp := range nums {
		sd += math.Pow(tmp-mn, 2)
	}
	sd = math.Sqrt(sd / float64(len(nums)))
	return sd
}

func (self *RiskParamDef) evaluate(positions []*Position, params map[string]interface{}, optional ...*Expression) interface{} {
	var e *Expression
	var isFormula bool
	if len(optional) > 0 {
		e = optional[0]
	} else {
		e = self.Formula
		isFormula = true
		if e.A == "" {
			// by default, only return top 10 result
			e.A = "top"
			e.N = 10
		}
	}
	if e.A == "call" {
		res, _ := CallPy(e.C[0], e.C[1], e.C[2], positions, self.Parent.Path)
		return res
	}
	value := math.NaN()
	res := make([]float64, 0, len(positions))
	for _, p := range positions {
		if isFormula {
			// prepare non-aggregate variable
			for _, v := range self.Variables {
				if v.E.A == "" {
					params[v.Name], _ = Evaluate(v.E, p, params)
				}
			}
		}
		tmp, _ := Evaluate(e, p, params)
		res = append(res, tmp.(float64))
	}
	if e.A == "std" {
		value = std(res)
	} else if e.A == "sum" {
		value = sum(res)
	} else if e.A == "len" {
		value = length(res)
	} else if e.A == "mean" {
		value = mean(res)
	} else if e.A == "top" {
		tmp := make([][2]interface{}, 0, len(Positions))
		for i, p := range positions {
			if math.IsNaN(res[i]) {
				continue
			}
			tmp = append(tmp, [2]interface{}{p.Security.Symbol, res[i]})
		}
		// will optimize with nth_element
		n := 0
		if e.N > 0 {
			sort.Slice(tmp, func(i, j int) bool { return tmp[i][1].(float64) > tmp[j][1].(float64) })
			n = e.N
		} else if e.N < 0 {
			sort.Slice(tmp, func(i, j int) bool { return tmp[i][1].(float64) < tmp[j][1].(float64) })
			n = -e.N
		}
		if n < len(tmp) {
			tmp = tmp[:n]
		}
		return tmp
	}
	if math.IsNaN(value) {
		// json Marshal failed to work with NaN, so change to string
		return "NaN"
	}
	return value
}

func (self *RiskParamDef) Run(gname string, positions []*Position) interface{} {
	var params map[string]interface{}
	// prepare aggregate variable
	if len(self.Variables) > 0 {
		params = make(map[string]interface{}, 60)
		for _, v := range self.Variables {
			if v.E.A != "" {
				params[v.Name] = self.evaluate(positions, params, v.E)
			}
		}
	}
	v := self.evaluate(positions, params)
	if self.Graph {
		if v2, ok2 := v.(float64); ok2 {
			tmp := self.History[gname]
			n := len(tmp)
			now := float64(time.Now().Unix())
			if n > 1 && now-tmp[0][0] > 25*3600 { // reduce history every 1h
				for i := 1; i < n; i += 1 {
					if now-tmp[i][0] < 24*3600 {
						tmp = tmp[i:]
						n = len(tmp)
						break
					}
				}
			}
			if n > 1 {
				tmp1 := tmp[n-2]
				tmp2 := tmp[n-1]
				if now-tmp1[0] > 60. && math.Abs(tmp1[1]-v2) > math.Abs(tmp1[1]+v2)/2000. {
					self.History[gname] = append(tmp, [2]float64{now, v2})
				} else {
					tmp2[0] = now
					tmp2[1] = v2
				}
			} else {
				self.History[gname] = append(tmp, [2]float64{now, v2})
			}
		}
	}
	return v
}
