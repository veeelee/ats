package main

import (
	//"cline"
	"command"
	//"configuration"
	"context"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"rut"
	"sort"
	"strconv"
	"strings"
	"util"
)

/*
 V5624G use Quad mode. (Intra slice + Inter Slice)
 N, N+128, N+256, N+384

 一条完整的flow是由FP_TCAM和FP_GLOBAL_MASK_TCAM两张表获取的

 模式的控制（single/double/quad）是由FP_PORT_FIELD_SEL来控制的.
 Intra-slice Pairing : SLICE(X)_DOUBLE_WIDE_MODE=1
 Inter-slice Pairing : SLICE(X)_(X-1)_PAIRING=1

 Intra-slice Double Wide Mode时，TCAM A的F1， F2， F3， F4是由FP_PORT_FIELD_SEL中相应的值控制的。
 Intra-slice Double Wide Mode时，TCAM B的F1， F2， F3， F4是由:
			F1 ---> FP_DOUBLE_WIDE_SELECT.SLICE_X_F1 控制
			F2 ---> FP_PORT_FIELD_SEL.SLICEx_DOUBLE_WIDE_F2_KEY_SELECT 控制。
			F4 ---> FP_DOUBLE_WIDE_SELECT.SLICE_x_F4控制
*/

var FP_DOUBLE_WIDE_SELECT = map[string]int64{
	"SLICE_9_F4":   0,
	"SLICE_9_F1":   0,
	"SLICE_8_F4":   0,
	"SLICE_8_F1":   0,
	"SLICE_7_F4":   0,
	"SLICE_7_F1":   0,
	"SLICE_6_F4":   0,
	"SLICE_6_F1":   0,
	"SLICE_5_F4":   0,
	"SLICE_5_F1":   0,
	"SLICE_4_F4":   0,
	"SLICE_4_F1":   0,
	"SLICE_3_F4":   0,
	"SLICE_3_F1":   0,
	"SLICE_2_F4":   0,
	"SLICE_2_F1":   0,
	"SLICE_1_F4":   0,
	"SLICE_1_F1":   0,
	"SLICE_15_F4":  0,
	"SLICE_15_F1":  0,
	"SLICE_14_F4":  0,
	"SLICE_14_F1":  0,
	"SLICE_13_F4":  0,
	"SLICE_13_F1":  0,
	"SLICE_12_F4":  0,
	"SLICE_12_F1":  0,
	"SLICE_11_F4":  0,
	"SLICE_11_F1":  0,
	"SLICE_10,_F4": 0,
	"SLICE_10,_F1": 0,
	"SLICE_0,_F4":  0,
	"SLICE_0,_F1":  0,
}

type TLV struct {
	Name   string
	Size   int
	Offset int
	Value  string
}

type RuleField struct {
	Key  []TLV
	Data string
	Mask string
}

const (
	FP_SINGLE_MODE = iota
	FP_INTRA_SLICE_PAIRING_MODE
	FP_INTER_SLICE_PAIRING_MODE
	FP_QUAD_MODE
	FP_AUTO_MODE
)

const (
	FP0 = iota
	FP1
	FP2
	FP3
	FP4
	DWFP0
	DWFP1
	DWFP2
	DWFP3
	DWFP4
	FIXED
	IPBM
)

//FP_TCAM.*[1]: <VALID=3,MASK=0x1fffe000000000000000000,KEY=0x10dba000000000000000000,F4_MASK=0,F4=0,F3_MASK=0,F3=0,F2_MASK=0xffff0000,F2=0x86dd0000,F1_MASK=0,F1=0,DW_DOUBLE_WIDE_MODE_MASK=0,DW_DOUBLE_WIDE_MODE=0,DWF4_MASK=0,DWF4=0,DWF3_MASK=0,DWF3=0,DWF2_MASK=0x01fffe00000000,DWF2=0x010dba00000000,DWF1_MASK=0,DWF1=0,DOUBLE_WIDE_MODE_MASK=0,DOUBLE_WIDE_MODE=0,DATA_MASK=0xffff000000000000000000,DATA_KEY=0x86dd000000000000000000>
//FP_GLOBAL_MASK_TCAM.*[1]: <VALID=1,RESERVED_SINGLE_WIDE_MASK=0,RESERVED_SINGLE_WIDE=0,MASK=0x3c3ffe001fffffff,KEY=0x3ffe001fffffff,IPBM_MASK=0x3c3ffe001fffffff,IPBM=0x3ffe001fffffff,FIXED_MASK=0,FIXED_KEY=0,DOUBLE_WIDE_F0_MASK=0x3c3ffe001fffffff,DOUBLE_WIDE_F0=0x3ffe001fffffff>

//RulePart stand for particular combined FP_TCAM and FP_GLOBAL mask entry.
//Each rule entry may contains multiple Parts.
//   1. Single Wide: 1
//   2. Double Wide: 2
//   3. Quad Wide:   4
type RulePart struct {
	Key                      map[int][]TLV
	Index                    int64
	FP0                      string
	FP0_MASK                 string
	FP1                      string
	FP1_MASK                 string
	FP2                      string
	FP2_MASK                 string
	FP3                      string
	FP3_MASK                 string
	FP4                      string
	FP4_MASK                 string
	DW_DOUBLE_WIDE_MODE      string
	DW_DOUBLE_WIDE_MODE_MASK string
	DOUBLE_WIDE_MODE         string
	DOUBLE_WIDE_MODE_MASK    string
	DOUBLE_WIDE_FP0          string
	DOUBLE_WIDE_FP0_MASK     string
	DWFP4                    string
	DWFP4_MASK               string
	DWFP3                    string
	DWFP3_MASK               string
	DWFP2                    string
	DWFP2_MASK               string
	DWFP1                    string
	DWFP1_MASK               string
	FIXED                    string
	FIXED_MASK               string
	IPBM                     string
	IPBM_MASK                string
	Policy                   *PolicyEntry
}

type RuleEntry struct {
	Index int64
	Parts []*RulePart
}

type RuleEntrySlice []*RuleEntry

func (res RuleEntrySlice) Len() int           { return len(res) }
func (res RuleEntrySlice) Swap(i, j int)      { res[i], res[j] = res[j], res[i] }
func (res RuleEntrySlice) Less(i, j int) bool { return res[i].Index < res[j].Index }

var PolicyEntryFields = []string{
	"Y_NEW_PKT_PRI",
	"Y_NEW_DSCP",
	"Y_DROP_PRECEDENCE",
	"Y_DROP",
	"Y_COS_INT_PRI",
	"Y_COPY_TO_CPU",
	"Y_CHANGE_PKT_PRI",
	"Y_CHANGE_ECN",
	"Y_CHANGE_DSCP",
	"Y_CHANGE_COS_OR_INT_PRI",
	"USE_SVC_METER_COLOR",
	"TC_PROFILE_INDEX",
	"SHARED_METER_PAIR_INDEX",
	"R_NEW_PKT_PRI",
	"R_NEW_DSCP",
	"R_DROP_PRECEDENCE",
	"R_DROP",
	"R_COS_INT_PRI",
	"R_COPY_TO_CPU",
	"R_CHANGE_PKT_PRI",
	"R_CHANGE_ECN",
	"R_CHANGE_DSCP",
	"R_CHANGE_COS_OR_INT_PRI",
	"RESERVED_193",
	"RESERVED_143",
	"RESERVED_0",
	"REDIRECTION_TYPE",
	"REDIRECTION_PROFILE_INDEX",
	"REDIRECTION_NHI",
	"REDIRECTION_NH",
	"REDIRECTION_DGLP",
	"REDIRECTION",
	"PROTECTION_SWITCHING_DROP_OVERIDE",
	"PPD1_CLASS_TAG",
	"OLP_HDR_TYPE_COMPRESSED",
	"OLP_HDR_ADD",
	"OAM_UP_MEP",
	"OAM_TX",
	"OAM_TUNNEL_CONTROL",
	"OAM_TAG_STATUS_CHECK_CONTROL",
	"OAM_SESSION_ID",
	"OAM_SERVICE_PRI_MAPPING_PTR",
	"OAM_LM_EN",
	"OAM_LM_BASE_PTR",
	"OAM_LMEP_MDL",
	"OAM_LMEP_EN",
	"OAM_ENABLE_LM_DM_SAMPLE",
	"OAM_DM_TYPE",
	"OAM_DM_EN",
	"NHI_GROUP_EN",
	"NEXT_HOP_INDEX",
	"MTP_INDEX3",
	"MTP_INDEX2",
	"MTP_INDEX1",
	"MTP_INDEX0",
	"MIRROR_OVERRIDE",
	"MIRROR",
	"METER_SHARING_MODE_MODIFIER",
	"METER_SHARING_MODE",
	"METER_PAIR_MODE_MODIFIER",
	"METER_PAIR_MODE",
	"METER_PAIR_INDEX",
	"MATCHED_RULE",
	"IM1_MTP_INDEX",
	"IM0_MTP_INDEX",
	"I2E_CLASSID_SEL",
	"I2E_CLASSID",
	"HG_CLASSID_SEL",
	"G_PACKET_REDIRECTION",
	"G_NEW_PKT_PRI",
	"G_NEW_DSCP_TOS",
	"G_L3SW_CHANGE_MACDA_OR_VLAN",
	"G_L3SW_CHANGE_L2_FIELDS",
	"G_DROP_PRECEDENCE",
	"G_DROP",
	"G_COS_INT_PRI",
	"G_COPY_TO_CPU",
	"G_CHANGE_PKT_PRI",
	"G_CHANGE_ECN",
	"G_CHANGE_DSCP_TOS",
	"G_CHANGE_COS_OR_INT_PRI",
	"GREEN_TO_PID",
	"EVEN_PARITY_1_DOMAIN",
	"EVEN_PARITY_1",
	"EVEN_PARITY_0_DOMAIN",
	"EVEN_PARITY_0",
	"EM1_MTP_INDEX",
	"EM0_MTP_INDEX",
	"EH_TAG_TYPE",
	"EH_QUEUE_TAG",
	"ECMP_PTR",
	"ECMP_NH_INFO",
	"ECMP_HASH_SEL",
	"ECMP",
	"DO_NOT_URPF",
	"DO_NOT_CHANGE_TTL",
	"CPU_COS",
	"COUNTER_MODE",
	"COUNTER_INDEX",
	"CHANGE_CPU_COS",
}

/*
var PolicyEntryFields = []string{
	"Y_NEW_PKT_PRI",
	"Y_NEW_ECN",
	"Y_NEW_DSCP",
	"Y_DROP_PRECEDENCE",
	"Y_DROP",
	"Y_COUNTER_OFFSET",
	"Y_COS_INT_PRI",
	"Y_COPY_TO_CPU",
	"Y_CHANGE_PKT_PRI",
	"Y_CHANGE_ECN",
	"Y_CHANGE_DSCP",
	"Y_CHANGE_COS_OR_INT_PRI",
	"USE_SVC_METER_COLOR",
	"UNICAST_REDIRECT_CONTROL",
	"SUPPRESS_SW_ACTIONS",
	"SUPPRESS_COLOR_SENSITIVE_ACTIONS",
	"SPLIT_DROP_RESOLVE",
	"SHARED_METER_SET",
	"SHARED_METER_PAIR_POOL_RESERVED_1",
	"SHARED_METER_PAIR_POOL_RESERVED_0",
	"SHARED_METER_PAIR_POOL_NUMBER",
	"SHARED_METER_PAIR_POOL_INDEX",
	"SHARED_METER_PAIR_MODE",
	"R_NEW_PKT_PRI",
	"R_NEW_ECN",
	"R_NEW_DSCP",
	"R_DROP_PRECEDENCE",
	"R_DROP",
	"R_COUNTER_OFFSET",
	"R_COS_INT_PRI",
	"R_COPY_TO_CPU",
	"R_CHANGE_PKT_PRI",
	"R_CHANGE_ECN",
	"R_CHANGE_DSCP",
	"R_CHANGE_COS_OR_INT_PRI",
	"RESERVED",
	"REDIRECT_USE_IFP_PBM",
	"REDIRECT_T",
	"REDIRECT_SET",
	"REDIRECT_PORT_NUM",
	"REDIRECT_NHI",
	"REDIRECT_MODID",
	"REDIRECT_L3MC_INDEX",
	"REDIRECT_L2MC_INDEX",
	"REDIRECT_INDEX_TYPE",
	"REDIRECT_IFP_PROFILE_INDEX",
	"REDIRECT_ECMP_GROUP",
	"REDIRECT_DVP",
	"REDIRECT_DGLP",
	"REDIRECTION",
	"PROTECTION_SWITCHING_DROP_OVERIDE",
	"PROFILE_SET",
	"PPD3_CLASS_TAG",
	"PPD1_CLASS_TAG",
	"OAM_UP_MEP",
	"OAM_TX",
	"OAM_TUNNEL_CONTROL",
	"OAM_TAG_STATUS_CHECK_CONTROL",
	"OAM_SET",
	"OAM_SERVICE_PRI_MAPPING_PTR",
	"OAM_LM_EN",
	"OAM_LM_BASE_PTR",
	"OAM_LMEP_MDL",
	"OAM_LMEP_EN",
	"OAM_ENABLE_LM_DM_SAMPLE",
	"OAM_DM_TYPE",
	"OAM_DM_EN",
	"NEXT_HOP_INDEX",
	"MTP_INDEX3",
	"MTP_INDEX2",
	"MTP_INDEX1",
	"MTP_INDEX0",
	"MISC_2_SET",
	"MISC_1_SET",
	"MIRROR_SET",
	"MIRROR_OVERRIDE",
	"MIRROR",
	"METER_SHARING_MODE_MODIFIER",
	"METER_SHARING_MODE",
	"METER_SET",
	"METER_PAIR_POOL_RESERVED_1",
	"METER_PAIR_POOL_RESERVED_0",
	"METER_PAIR_POOL_NUMBER",
	"METER_PAIR_POOL_INDEX",
	"METER_PAIR_MODE_MODIFIER",
	"METER_PAIR_MODE",
	"MATCHED_RULE",
	"LAG_DLB_DISABLE",
	"L3SW_L2_FIELDS_SET",
	"L2MOD_TBL_INDEX",
	"I2E_CLASSID_SEL",
	"I2E_CLASSID",
	"HI_PRI_SUPPRESS_VXLT",
	"HI_PRI_RESOLVE",
	"HI_PRI_ACTION_CONTROL",
	"HG_CLASSID_SEL",
	"HGT_DLB_DISABLE",
	"G_PACKET_REDIRECTION",
	"G_NEW_PKT_PRI",
	"G_NEW_ECN",
	"G_NEW_DSCP_TOS",
	"G_L3SW_CHANGE_L2_FIELDS",
	"G_DROP_PRECEDENCE",
	"G_DROP",
	"G_COUNTER_OFFSET",
	"G_COS_INT_PRI",
	"G_COPY_TO_CPU",
	"G_CHANGE_PKT_PRI",
	"G_CHANGE_ECN",
	"G_CHANGE_DSCP_TOS",
	"G_CHANGE_COS_OR_INT_PRI",
	"GREEN_TO_PID",
	"GOA_SET",
	"FP_POLICY_TABLE_B",
	"FP_POLICY_TABLE_A",
	"EVEN_PARITY_B",
	"EVEN_PARITY_A",
	"EH_TM",
	"EH_TAG_TYPE",
	"EH_QUEUE_TAG",
	"ECMP_PTR",
	"ECMP_NH_INFO",
	"ECMP_HASH_SEL",
	"ECMP",
	"DO_NOT_URPF",
	"DO_NOT_GENERATE_CNM",
	"DO_NOT_CHANGE_TTL",
	"DEFER_QOS_MARKINGS",
	"CPU_COS_SET",
	"CPU_COS",
	"COUNTER_SET",
	"COUNTER_POOL_NUMBER",
	"COUNTER_POOL_INDEX",
	"COPY_TO_PASSTHRU_NLF",
	"COPY_TO_CPU_SET",
	"CHANGE_CPU_COS",
}


var PolicyEntryFields = []string{
	"Y_NEW_PKT_PRI",
	"Y_NEW_ECN",
	"Y_NEW_DSCP",
	"Y_DROP_PRECEDENCE",
	"Y_DROP",
	"Y_COS_INT_PRI",
	"Y_COPY_TO_CPU",
	"Y_CHANGE_PKT_PRI",
	"Y_CHANGE_ECN",
	"Y_CHANGE_DSCP",
	"Y_CHANGE_COS_OR_INT_PRI",
	"UNICAST_REDIRECT_CONTROL",
	"SUPPRESS_VXLT",
	"SFLOW_ING_SAMPLE",
	"SFLOW_EGR_SAMPLE",
	"R_NEW_PKT_PRI",
	"R_NEW_ECN",
	"R_NEW_DSCP",
	"R_DROP_PRECEDENCE",
	"R_DROP",
	"R_COS_INT_PRI",
	"R_COPY_TO_CPU",
	"R_CHANGE_PKT_PRI",
	"R_CHANGE_ECN",
	"R_CHANGE_DSCP",
	"R_CHANGE_COS_OR_INT_PRI",
	"RSVD_NEXT_HOP_INDEX",
	"RESERVED_EH_TM",
	"RESERVED_3",
	"RESERVED_2",
	"RESERVED_1",
	"RESERVED_0",
	"REDIRECT_USE_IFP_PBM",
	"REDIRECT_T",
	"REDIRECT_PORT_NUM",
	"REDIRECT_NHI",
	"REDIRECT_MODID",
	"REDIRECT_L3MC_INDEX",
	"REDIRECT_L2MC_INDEX",
	"REDIRECT_INDEX_TYPE",
	"REDIRECT_IFP_PROFILE_INDEX",
	"REDIRECT_ECMP_GROUP",
	"REDIRECT_DVP",
	"REDIRECT_DGLP",
	"REDIRECTION",
	"PPD3_CLASS_TAG",
	"OAM_UP_MEP",
	"OAM_TX",
	"OAM_TUNNEL_CONTROL",
	"OAM_TAG_STATUS_CHECK_CONTROL",
	"OAM_SERVICE_PRI_MAPPING_PTR",
	"OAM_LM_EN",
	"OAM_LM_BASE_PTR",
	"OAM_LMEP_MDL",
	"OAM_LMEP_EN",
	"OAM_ENABLE_LM_DM_SAMPLE",
	"OAM_DM_TYPE",
	"OAM_DM_EN",
	"NEXT_HOP_INDEX",
	"NAT_PACKET_EDIT_IDX",
	"NAT_PACKET_EDIT_ENTRY_SEL",
	"NAT_ENABLE",
	"MTP_INDEX3",
	"MTP_INDEX2",
	"MTP_INDEX1",
	"MTP_INDEX0",
	"MIRROR_OVERRIDE",
	"MIRROR",
	"METER_UPDATE_ODD",
	"METER_UPDATE_EVEN",
	"METER_TEST_ODD",
	"METER_TEST_EVEN",
	"METER_PAIR_MODE_MODIFIER",
	"METER_PAIR_MODE",
	"METER_PAIR_INDEX_ODD",
	"METER_PAIR_INDEX_EVEN",
	"MATCHED_RULE",
	"LAG_RH_DISABLE",
	"I2E_CLASSID_SEL",
	"I2E_CLASSID",
	"HG_CLASSID_SEL",
	"HGT_RH_DISABLE",
	"HGT_DLB_DISABLE",
	"G_PACKET_REDIRECTION",
	"G_NEW_PKT_PRI",
	"G_NEW_ECN",
	"G_NEW_DSCP_TOS",
	"G_L3SW_CHANGE_L2_FIELDS",
	"G_DROP_PRECEDENCE",
	"G_DROP",
	"G_COS_INT_PRI",
	"G_COPY_TO_CPU",
	"G_CHANGE_PKT_PRI",
	"G_CHANGE_ECN",
	"G_CHANGE_DSCP_TOS",
	"G_CHANGE_COS_OR_INT_PRI",
	"GREEN_TO_PID",
	"FCOE_ZONE_CHECK_ACTION",
	"FCOE_VSAN_PRI_VALID",
	"FCOE_VSAN_PRI",
	"FCOE_VSAN_ID",
	"EVEN_PARITY",
	"EH_TAG_TYPE",
	"EH_QUEUE_TAG",
	"ECMP_RH_DISABLE",
	"ECMP_PTR",
	"ECMP_NH_INFO",
	"ECMP_HASH_SEL",
	"ECMP",
	"DO_NOT_URPF",
	"DO_NOT_NAT",
	"DO_NOT_GENERATE_CNM",
	"DO_NOT_CUT_THROUGH",
	"DO_NOT_CHANGE_TTL",
	"CPU_COS",
	"COUNTER_MODE",
	"COUNTER_INDEX",
	"CHANGE_CPU_COS",
}
*/

type PolicyEntry struct {
	Fields map[string]int64
}

type FieldSlice []map[string]int64

func (fs FieldSlice) Len() int      { return len(fs) }
func (fs FieldSlice) Swap(i, j int) { fs[i], fs[j] = fs[j], fs[i] }
func (fs FieldSlice) Less(i, j int) bool {
	var ik string
	var jk string
	for k, _ := range fs[i] {
		ik = k
		break
	}
	for k, _ := range fs[j] {
		jk = k
		break
	}

	return ik < jk
}

func (pe *PolicyEntry) String() string {
	var res string
	var fs FieldSlice = make([]map[string]int64, 0, 1)
	res += fmt.Sprintf("[")
	for k, field := range pe.Fields {
		if field != 0 {
			fs = append(fs, map[string]int64{k: field})
		}
	}

	sort.Sort(fs)

	for _, f := range fs {
		for k, field := range f {
			res += fmt.Sprintf(" %10s -> %2d ", k, field)
		}
	}
	res += fmt.Sprintf("]")

	return res
}

//Flow 配置的内容是由FP_GLOBAL_MASK_TCAM 和FP_TCAM两张表决定的, 所以Rule Entry的内容需要解析这两张表.
func (re *RuleEntry) String() string {
	var res string
	var Yellow = color.New(color.FgYellow)
	var Cyan = color.New(color.FgCyan)
	var Green = color.New(color.FgGreen)
	res += fmt.Sprintf("{[")
	for _, p := range re.Parts {
		res += Yellow.Sprintf("%05d(%d) ", p.Index, DB.EntryToSliceMap[p.Index])
	}
	res += fmt.Sprintf("\n")
	for _, p := range re.Parts {
		slice := DB.EntryToSliceMap[p.Index]
		if DB.PFS[0].SliceFieldSelectors[slice].DOUBLE_WIDE_MODE == 0 { //!Intra slice pairing
			res += Yellow.Sprintf("  [%05d(%d)]:\n", p.Index, DB.EntryToSliceMap[p.Index])
			res += fmt.Sprintf("     Key: \n")
			res += fmt.Sprintf("       FP1: %+v\n", p.Key[FP1])
			res += fmt.Sprintf("       FP2: %+v\n", p.Key[FP2])
			res += fmt.Sprintf("       FP3: %+v\n", p.Key[FP3])
			res += fmt.Sprintf("       FP4: %+v\n", p.Key[FP4])
			res += fmt.Sprintf("       FIXED: %+v\n", p.Key[FIXED])
			res += Cyan.Sprintf("     Field: \n")
			res += fmt.Sprintf("       FP1: %40s:  FP1_MASK: %40s\n", p.FP1, p.FP1_MASK)
			res += fmt.Sprintf("       FP2: %40s:  FP2_MASK: %40s\n", p.FP2, p.FP2_MASK)
			res += fmt.Sprintf("       FP3: %40s:  FP3_MASK: %40s\n", p.FP3, p.FP3_MASK)
			res += fmt.Sprintf("       FP4: %40s:  FP4_MASK: %40s\n", p.FP4, p.FP4_MASK)
			res += fmt.Sprintf("       FIXED: %+v\n", p.Key[FIXED])
			res += fmt.Sprintf("       IPBM: %39s:  IPBM_MASK: %39s\n", p.IPBM, p.IPBM_MASK)
			res += Green.Sprintf("   Action: \n")
			res += fmt.Sprintf("       %s\n", p.Policy)
		} else {
			if p.Index < DB.SliceStartIndexMap[slice]+DB.SliceEntryCountMap[slice]/2 {
				res += Yellow.Sprintf("  [%05d(%d)]:\n", p.Index, DB.EntryToSliceMap[p.Index])
				res += fmt.Sprintf("     Key: \n")
				res += fmt.Sprintf("       FP1: %+v\n", p.Key[FP1])
				res += fmt.Sprintf("       FP2: %+v\n", p.Key[FP2])
				res += fmt.Sprintf("       FP3: %+v\n", p.Key[FP3])
				res += fmt.Sprintf("       FP4: %+v\n", p.Key[FP4])
				res += fmt.Sprintf("       FIXED: %+v\n", p.Key[FIXED])
				res += Cyan.Sprintf("     Field: \n")
				res += fmt.Sprintf("       FP1: %40s:  FP1_MASK: %40s\n", p.FP1, p.FP1_MASK)
				res += fmt.Sprintf("       FP2: %40s:  FP2_MASK: %40s\n", p.FP2, p.FP2_MASK)
				res += fmt.Sprintf("       FP3: %40s:  FP3_MASK: %40s\n", p.FP3, p.FP3_MASK)
				res += fmt.Sprintf("       FP4: %40s:  FP4_MASK: %40s\n", p.FP4, p.FP4_MASK)
				res += fmt.Sprintf("       FIXED: %+v\n", p.Key[FIXED])
				res += fmt.Sprintf("       IPBM: %39s:  IPBM_MASK: %39s\n", p.IPBM, p.IPBM_MASK)
				res += Green.Sprintf("   Action: \n")
				res += fmt.Sprintf("       %s\n", p.Policy)

			} else {
				res += Yellow.Sprintf("  [%05d(%d)]:\n", p.Index, DB.EntryToSliceMap[p.Index])
				res += fmt.Sprintf("     Key: \n")
				res += fmt.Sprintf("       FP1: %+v\n", p.Key[FP1])
				res += fmt.Sprintf("       FP2: %+v\n", p.Key[FP2])
				res += fmt.Sprintf("       FP3: %+v\n", p.Key[FP3])
				res += fmt.Sprintf("       FP4: %+v\n", p.Key[FP4])
				res += Cyan.Sprintf("     Field: \n")
				res += fmt.Sprintf("       DWFP1: %38s:  DWFP1_MASK: %38s\n", p.DWFP1, p.DWFP1_MASK)
				res += fmt.Sprintf("       DWFP2: %38s:  DWFP2_MASK: %38s\n", p.DWFP2, p.DWFP2_MASK)
				res += fmt.Sprintf("       DWFP3: %38s:  DWFP3_MASK: %38s\n", p.DWFP3, p.DWFP3_MASK)
				res += fmt.Sprintf("       DWFP4: %38s:  DWFP4_MASK: %38s\n", p.DWFP4, p.DWFP4_MASK)
				res += Green.Sprintf("   Action: \n")
				res += fmt.Sprintf("       %s\n", p.Policy)
			}
		}
	}
	res += fmt.Sprintf("}")

	return res
}

type RuleRawEntry struct {
	Index               int64
	FP_TCAM             string
	FP_GLOBAL_MASK_TCAM string
	FP_POLICY_TABLE     string
}

type RuleDB struct {
	Device             *rut.RUT
	SliceCount         int
	VirtualSliceMap    map[int64]int64 /* key physicla slice, value virtual slice */
	GroupCount         int
	SliceGroupMap      map[int64]int64 /* key slice, value group */
	SliceEntryCountMap map[int64]int64
	SliceStartIndexMap map[int64]int64
	SliceEndIndexMap   map[int64]int64
	EntryToSliceMap    map[int64]int64
	EntryCount         int64
	PFS                map[int64]*PortFieldSelector
	RuleEntries        map[int64]*RuleEntry
	RuleEntriesOrdered RuleEntrySlice
	RawEntries         map[int64]*RuleRawEntry
	RawTables          map[string]string
	Mode               int
}

func (rdb *RuleDB) Clear() {
	rdb.Mode = FP_QUAD_MODE
	rdb.SliceCount = 0
	rdb.GroupCount = 0
	rdb.EntryCount = 0
	rdb.VirtualSliceMap = make(map[int64]int64, 1)
	rdb.SliceGroupMap = make(map[int64]int64, 1)
	rdb.SliceEntryCountMap = make(map[int64]int64, 1)
	rdb.SliceStartIndexMap = make(map[int64]int64, 1)
	rdb.SliceEndIndexMap = make(map[int64]int64, 1)
	rdb.EntryToSliceMap = make(map[int64]int64, 1)
	rdb.PFS = make(map[int64]*PortFieldSelector, 1)
	rdb.RuleEntries = make(map[int64]*RuleEntry, 1)
	rdb.RawEntries = make(map[int64]*RuleRawEntry, 1)
	rdb.RawTables = make(map[string]string, 1)
	rdb.RuleEntriesOrdered = make([]*RuleEntry, 1)
}

func (rdb *RuleDB) IsInitialized() bool {
	if _, ok := rdb.RawTables["FP_TCAM"]; ok {
		if _, ok := rdb.RawTables["FP_GLOBAL_MASK_TCAM"]; ok {
			if _, ok := rdb.RawTables["FP_POLICY_TABLE"]; ok {
			}
			return true
		}
	}

	return false
}

var FPTCAMIndexReg = regexp.MustCompile(`FP_TCAM\.ipipe0\[(?P<index>[0]*[xX]*[0-9a-fA-F]+)\]:`)

var FPGlobalMaskTCAMEntryRegFmt = `FP_GLOBAL_MASK_TCAM\.ipipe0\[%d\]:[[:space:]]*<[a-zA-Z0-9,=_[:space:]]+>`
var FPPolicyTableEntryRegFmt = `FP_POLICY_TABLE\.ipipe0\[%d\]:[[:space:]]*<[a-zA-Z0-9,=_[:space:]+]+>`

func (rdb *RuleDB) GetRawEntries() error {
	if !rdb.IsInitialized() {
		panic("Cannot get rule entries, DB not initilaized")
	}

	//Clear the Raw DB before a new iteration
	rdb.RawEntries = make(map[int64]*RuleRawEntry, 1)

	ft := rdb.RawTables["FP_TCAM"]
	fgmt := rdb.RawTables["FP_GLOBAL_MASK_TCAM"]
	fpt := rdb.RawTables["FP_POLICY_TABLE"]

	lines := strings.Split(string(ft), "\r\n")
	for _, line := range lines {
		var rule RuleRawEntry
		if strings.HasPrefix(line, "FP_TCAM") && strings.Contains(line, "VALID=3") {
			rule.FP_TCAM = line
			match := FPTCAMIndexReg.FindStringSubmatch(string(line))
			rule.Index, _ = strconv.ParseInt(match[1], 0, 32)

			fgmtEntryReg := regexp.MustCompile(fmt.Sprintf(FPGlobalMaskTCAMEntryRegFmt, rule.Index))
			fgmtEntry := fgmtEntryReg.FindStringSubmatch(string(fgmt))
			rule.FP_GLOBAL_MASK_TCAM = fgmtEntry[0]

			fptEntryReg := regexp.MustCompile(fmt.Sprintf(FPPolicyTableEntryRegFmt, rule.Index))
			fptEntry := fptEntryReg.FindStringSubmatch(string(fpt))
			rule.FP_POLICY_TABLE = fptEntry[0]

			rdb.RawEntries[rule.Index] = &rule
		}
	}

	return nil
}

//Datasheet中的Double Wide Mode指的是intra-slice double wide mode.
//此时TCAM-A使用single wide key, TCAM-B使用double wide key.
//注意TCAMA和TCAMB是位于同一个slice中的
func (rdb *RuleDB) ParseRawEntries() {
	if !rdb.IsInitialized() {
		panic("Cannot parse rule entries, DB not initilaized")
	}

	rdb.RuleEntries = make(map[int64]*RuleEntry, 1)
	rdb.RuleEntriesOrdered = make([]*RuleEntry, 0, 1)
	for index, rr := range rdb.RawEntries {
		slice := DB.EntryToSliceMap[index]
		if slice%2 == 0 { //Even slice
			if rdb.PFS[0].SliceFieldSelectors[slice].DOUBLE_WIDE_MODE == 0 { //!Intra slice pairing
				if rdb.PFS[0].SliceFieldSelectors[slice+1].PAIRING_EVEN_SLICE == 0 {
					var entry RuleEntry
					entry.Index = index
					entry.Parts = make([]*RulePart, 0, 1)
					entry.Parts = append(entry.Parts, rdb.ParseRawEntry(rr))
					rdb.RuleEntries[index] = &entry
					rdb.RuleEntriesOrdered = append(rdb.RuleEntriesOrdered, rdb.RuleEntries[index])
				} else { //Inter slice pairing
					var entry RuleEntry
					entry.Index = index
					entry.Parts = make([]*RulePart, 0, 1)
					entry.Parts = append(entry.Parts, rdb.ParseRawEntry(rr))
					entry.Parts = append(entry.Parts, rdb.ParseRawEntry(DB.RawEntries[index+DB.SliceEntryCountMap[slice]]))
					rdb.RuleEntries[index] = &entry
					rdb.RuleEntriesOrdered = append(rdb.RuleEntriesOrdered, rdb.RuleEntries[index])
				}
			} else {
				if rdb.PFS[0].SliceFieldSelectors[slice+1].PAIRING_EVEN_SLICE == 0 {
					if index < DB.SliceStartIndexMap[slice]+DB.SliceEntryCountMap[slice]/2 {
						var entry RuleEntry
						entry.Index = index
						entry.Parts = make([]*RulePart, 0, 1)
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(rr))
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(DB.RawEntries[index+DB.SliceEntryCountMap[slice]/2]))
						rdb.RuleEntries[index] = &entry
						rdb.RuleEntriesOrdered = append(rdb.RuleEntriesOrdered, rdb.RuleEntries[index])
					}
				} else {
					if index < DB.SliceStartIndexMap[slice]+DB.SliceEntryCountMap[slice]/2 {
						var entry RuleEntry
						entry.Index = index
						entry.Parts = make([]*RulePart, 0, 1)
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(rr))
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(DB.RawEntries[index+DB.SliceEntryCountMap[slice]/2]))
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(DB.RawEntries[index+DB.SliceEntryCountMap[slice]]))
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(DB.RawEntries[index+DB.SliceEntryCountMap[slice]+DB.SliceEntryCountMap[slice]/2]))
						rdb.RuleEntries[index] = &entry
						rdb.RuleEntriesOrdered = append(rdb.RuleEntriesOrdered, rdb.RuleEntries[index])
					}
				}
			}
		} else {
			if rdb.PFS[0].SliceFieldSelectors[slice].PAIRING_EVEN_SLICE == 0 {
				if rdb.PFS[0].SliceFieldSelectors[slice].DOUBLE_WIDE_MODE == 0 {
					var entry RuleEntry
					entry.Index = index
					entry.Parts = make([]*RulePart, 0, 1)
					entry.Parts = append(entry.Parts, rdb.ParseRawEntry(rr))
					rdb.RuleEntries[index] = &entry
					rdb.RuleEntriesOrdered = append(rdb.RuleEntriesOrdered, rdb.RuleEntries[index])
				} else {
					if index < DB.SliceStartIndexMap[slice]+DB.SliceEntryCountMap[slice]/2 {
						var entry RuleEntry
						entry.Index = index
						entry.Parts = make([]*RulePart, 0, 1)
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(rr))
						entry.Parts = append(entry.Parts, rdb.ParseRawEntry(DB.RawEntries[index+DB.SliceEntryCountMap[slice]/2]))
						rdb.RuleEntries[index] = &entry
						rdb.RuleEntriesOrdered = append(rdb.RuleEntriesOrdered, rdb.RuleEntries[index])
					}
				}
			}
		}
	}

	sort.Sort(rdb.RuleEntriesOrdered)
}

func (rdb *RuleDB) ParseRawEntry(raw *RuleRawEntry) *RulePart {
	var part RulePart
	part.Index = raw.Index
	part.Key = make(map[int][]TLV, 1)
	if rdb.Mode == FP_SINGLE_MODE {
		part.Key[FP1] = BCM56450ICAPFieldSelector_TCAMA.FP1[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP1)]
		part.Key[FP2] = BCM56450ICAPFieldSelector_TCAMA.FP2[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP2)]
		part.Key[FP3] = BCM56450ICAPFieldSelector_TCAMA.FP3[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP3)]
		part.Key[FP4] = BCM56450ICAPFieldSelector_TCAMA.FP4[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP4)]
		part.Key[FIXED] = BCM56450ICAPFieldSelector_TCAMA.FIXED[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].PAIRING_FIXED)]
	} else if rdb.Mode == FP_INTRA_SLICE_PAIRING_MODE {
		if part.Index < rdb.SliceStartIndexMap[rdb.EntryToSliceMap[part.Index]]+rdb.SliceEntryCountMap[rdb.EntryToSliceMap[part.Index]]/2 {
			//TCAM A
			part.Key[FP1] = BCM56450ICAPFieldSelector_TCAMA.FP1[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP1)]
			part.Key[FP2] = BCM56450ICAPFieldSelector_TCAMA.FP2[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP2)]
			part.Key[FP3] = BCM56450ICAPFieldSelector_TCAMA.FP3[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP3)]
			part.Key[FP4] = BCM56450ICAPFieldSelector_TCAMA.FP4[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP4)]
			part.Key[FIXED] = BCM56450ICAPFieldSelector_TCAMA.FIXED[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].PAIRING_FIXED)]
		} else { //TCAM B
			part.Key[FP0] = BCM56450ICAPFieldSelector_TCAMB.FP0[0]
			part.Key[FP1] = BCM56450ICAPFieldSelector_TCAMB.FP1[int(FP_DOUBLE_WIDE_SELECT[fmt.Sprintf("SLICE%d_F1", part.Index)])]
			part.Key[FP2] = BCM56450ICAPFieldSelector_TCAMB.FP2[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].DOUBLE_WIDE_F2_KEY_SELECT)]
			part.Key[FP3] = BCM56450ICAPFieldSelector_TCAMB.FP3[0]
			part.Key[FP4] = BCM56450ICAPFieldSelector_TCAMB.FP1[int(FP_DOUBLE_WIDE_SELECT[fmt.Sprintf("SLICE%d_F4", part.Index)])]
		}
	} else if rdb.Mode == FP_INTER_SLICE_PAIRING_MODE {
		part.Key[FP1] = BCM56450ICAPFieldSelector_TCAMA.FP1[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP1)]
		part.Key[FP2] = BCM56450ICAPFieldSelector_TCAMA.FP2[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP2)]
		part.Key[FP3] = BCM56450ICAPFieldSelector_TCAMA.FP3[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP3)]
		part.Key[FP4] = BCM56450ICAPFieldSelector_TCAMA.FP4[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP4)]
		part.Key[FIXED] = BCM56450ICAPFieldSelector_TCAMA.FIXED[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].PAIRING_FIXED)]
	} else if rdb.Mode == FP_QUAD_MODE {
		if part.Index < rdb.SliceStartIndexMap[rdb.EntryToSliceMap[part.Index]]+rdb.SliceEntryCountMap[rdb.EntryToSliceMap[part.Index]]/2 {
			//TCAM A
			part.Key[FP1] = BCM56450ICAPFieldSelector_TCAMA.FP1[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP1)]
			part.Key[FP2] = BCM56450ICAPFieldSelector_TCAMA.FP2[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP2)]
			part.Key[FP3] = BCM56450ICAPFieldSelector_TCAMA.FP3[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP3)]
			part.Key[FP4] = BCM56450ICAPFieldSelector_TCAMA.FP4[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].FP4)]
			part.Key[FIXED] = BCM56450ICAPFieldSelector_TCAMA.FIXED[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].PAIRING_FIXED)]
		} else { //TCAM B
			part.Key[FP0] = BCM56450ICAPFieldSelector_TCAMB.FP0[0]
			part.Key[FP1] = BCM56450ICAPFieldSelector_TCAMB.FP1[int(FP_DOUBLE_WIDE_SELECT[fmt.Sprintf("SLICE%d_F1", part.Index)])]
			part.Key[FP2] = BCM56450ICAPFieldSelector_TCAMB.FP2[int(rdb.PFS[0].SliceFieldSelectors[rdb.EntryToSliceMap[part.Index]].DOUBLE_WIDE_F2_KEY_SELECT)]
			part.Key[FP3] = BCM56450ICAPFieldSelector_TCAMB.FP3[0]
			part.Key[FP4] = BCM56450ICAPFieldSelector_TCAMB.FP1[int(FP_DOUBLE_WIDE_SELECT[fmt.Sprintf("SLICE%d_F4", part.Index)])]
		}
	}

	/* Get F1, F2, F3, F4 from FP_TCAM */
	f1match := FPTCAMEntryF1Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(f1match) > 1 {
		part.FP1 = f1match[2]
		part.FP1_MASK = f1match[1]
	}

	f2match := FPTCAMEntryF2Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(f2match) > 1 {
		part.FP2 = f2match[2]
		part.FP2_MASK = f2match[1]
	}

	f3match := FPTCAMEntryF3Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(f3match) > 1 {
		part.FP3 = f3match[2]
		part.FP3_MASK = f3match[1]
	}

	f4match := FPTCAMEntryF4Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(f4match) > 1 {
		part.FP4 = f4match[2]
		part.FP4_MASK = f4match[1]
	}

	df1match := FPTCAMEntryDWF1Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(df1match) > 1 {
		part.DWFP1 = df1match[2]
		part.DWFP1_MASK = df1match[1]
	}

	df2match := FPTCAMEntryDWF2Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(df2match) > 1 {
		part.DWFP2 = df2match[2]
		part.DWFP2_MASK = df2match[1]
	}

	df3match := FPTCAMEntryDWF3Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(df3match) > 1 {
		part.DWFP3 = df3match[2]
		part.DWFP3_MASK = df3match[1]
	}

	df4match := FPTCAMEntryDWF4Reg.FindStringSubmatch(raw.FP_TCAM)
	if len(df4match) > 1 {
		part.DWFP4 = df4match[2]
		part.DWFP4_MASK = df4match[1]
	}

	dwmmatch := FPTCAMEntryDWDOUBLEWIDEMODEReg.FindStringSubmatch(raw.FP_TCAM)
	if len(dwmmatch) > 1 {
		part.DOUBLE_WIDE_MODE = dwmmatch[2]
		part.DOUBLE_WIDE_MODE_MASK = dwmmatch[1]
	}

	/* Get F0, Fixed, IPBM from FP_GLOBAL_MASK_TCAM */
	fixedmatch := FPGlobalMaskTCAMFIXEDReg.FindStringSubmatch(raw.FP_GLOBAL_MASK_TCAM)
	if len(fixedmatch) > 1 {
		part.FIXED = fixedmatch[2]
		part.FIXED_MASK = fixedmatch[1]
	}

	ipbmmatch := FPGlobalMaskTCAMIPBMReg.FindStringSubmatch(raw.FP_GLOBAL_MASK_TCAM)
	if len(ipbmmatch) > 1 {
		part.IPBM = ipbmmatch[2]
		part.IPBM_MASK = ipbmmatch[1]
	}

	part.Policy = rdb.parsePolicyEntry(raw.FP_POLICY_TABLE)

	return &part
}

var FieldValueRegFmt = `=(?P<value>0*[xX]*[0-9a-fA-F]+)`

func (rdb *RuleDB) parsePolicyEntry(line string) *PolicyEntry {
	var policy PolicyEntry
	policy.Fields = make(map[string]int64)
	for _, field := range PolicyEntryFields {
		matcher := regexp.MustCompile(field + FieldValueRegFmt)
		matches := matcher.FindStringSubmatch(line)
		policy.Fields[field], _ = strconv.ParseInt(matches[1], 0, 32)
	}

	return &policy
}

var DB RuleDB = RuleDB{
	Mode:               FP_QUAD_MODE,
	VirtualSliceMap:    make(map[int64]int64, 1),
	SliceGroupMap:      make(map[int64]int64, 1),
	SliceEntryCountMap: make(map[int64]int64, 1),
	SliceStartIndexMap: make(map[int64]int64, 1),
	SliceEndIndexMap:   make(map[int64]int64, 1),
	EntryToSliceMap:    make(map[int64]int64, 1),
	PFS:                make(map[int64]*PortFieldSelector, 1),
	RuleEntries:        make(map[int64]*RuleEntry, 1),
	RawEntries:         make(map[int64]*RuleRawEntry, 1),
	RawTables:          make(map[string]string, 1),
	RuleEntriesOrdered: make([]*RuleEntry, 0, 1),
}

//Refer to AG201
//FP_SLICE_INDEX_CONTROL: Select Index for FP Port Field Select Table(UDF/Source Port)

type ICAPFieldSelector struct {
	FP0             map[int][]TLV
	FP1             map[int][]TLV
	FP2             map[int][]TLV
	FP3             map[int][]TLV
	FP4             map[int][]TLV
	FIXED           map[int][]TLV
	PAIRING_IPBM_F0 map[int][]TLV
	PAIRING_FIXED   map[int][]TLV
	IPBM            map[int][]TLV
}

var BCM56450ICAPFieldSelector_TCAMA = ICAPFieldSelector{
	FP1: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "SVP_L3_IIF", Size: 16, Offset: 24},
			TLV{Name: "FORWARDING_FIELD", Size: 14, Offset: 10},
			TLV{Name: "CLASSIDS", Size: 10, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 38},
			TLV{Name: "SVP_VALID", Size: 1, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "CLASSIDS", Size: 10, Offset: 34},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "OUTER_TPID_ENCODE", Size: 2, Offset: 32},
			TLV{Name: "INNER_VLAN_TAG", Size: 16, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},
		4: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},
		5: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "INNER_TPID_ENCODE", Size: 2, Offset: 42},
			TLV{Name: "INNER_VLAN_TAG", Size: 16, Offset: 26},
			TLV{Name: "LOOKUP_STATUS", Size: 20, Offset: 0},
		},
		6: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "IP_INFO", Size: 3, Offset: 31},
			TLV{Name: "PKT_RESOLUTION", Size: 6, Offset: 25},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 22},
			TLV{Name: "SWITCH_TAG_STATUS", Size: 2, Offset: 20},
			TLV{Name: "PACKET_FORMAT", Size: 4, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},
		7: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "RAL_GAL", Size: 2, Offset: 40},
			TLV{Name: "MPLS_INFO", Size: 2, Offset: 40},
			TLV{Name: "FORWARDING_FIELD", Size: 14, Offset: 26},
			TLV{Name: "CLASSIDS_11_6", Size: 10, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},
		8: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "FORWARDING_FIELD", Size: 14, Offset: 26},
			TLV{Name: "CLASSIDS_11_6", Size: 10, Offset: 16},
			TLV{Name: "TOS_FN", Size: 8, Offset: 8},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 0},
		},
		9: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "RAL_GAL", Size: 2, Offset: 32},
			TLV{Name: "UDF1_CHUNK_VALID_1_0", Size: 2, Offset: 34},
			TLV{Name: "MPLS_INFO", Size: 2, Offset: 32},
			TLV{Name: "UDF1_31_0", Size: 32, Offset: 0},
		},
		10: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "DGLP", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		11: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "INT_PRI", Size: 4, Offset: 33},
			TLV{Name: "CNG", Size: 4, Offset: 33},
			TLV{Name: "SVP_VALID", Size: 1, Offset: 32},
			TLV{Name: "SVP", Size: 16, Offset: 16},
			TLV{Name: "SGLP", Size: 16, Offset: 0},
		},
		12: []TLV{
			TLV{Name: "LABEL_ACTION", Size: 16, Offset: 24},
			TLV{Name: "SVP", Size: 16, Offset: 22},
			TLV{Name: "CLASSIDS", Size: 10, Offset: 12},
			TLV{Name: "OUTER_VLAN_ID", Size: 12, Offset: 0},
		},
		13: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "LABEL_ACTION", Size: 3, Offset: 33},
			TLV{Name: "CW_VALID", Size: 1, Offset: 32},
			TLV{Name: "AUX_TAG_VALID_1", Size: 32, Offset: 0},
			TLV{Name: "AUX_TAG_1", Size: 32, Offset: 0},
		},
	},
	FP2: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 96},
			TLV{Name: "DIP", Size: 32, Offset: 64},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 56},
			TLV{Name: "L4_SRC", Size: 16, Offset: 40},
			TLV{Name: "L4_DEST", Size: 16, Offset: 24},
			TLV{Name: "TOS_FN", Size: 8, Offset: 16},
			TLV{Name: "IPFLAG", Size: 2, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 96},
			TLV{Name: "DIP", Size: 32, Offset: 64},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 56},
			TLV{Name: "L4_SRC", Size: 16, Offset: 40},
			TLV{Name: "L4_DEST", Size: 16, Offset: 24},
			TLV{Name: "TOS_FN", Size: 8, Offset: 16},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "SIP", Size: 128, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "DIP", Size: 128, Offset: 0},
		},
		4: []TLV{
			TLV{Name: "DIP_127_64", Size: 64, Offset: 64},
			TLV{Name: "SPARE", Size: 8, Offset: 34},
			TLV{Name: "LAST_NH", Size: 8, Offset: 42},
			TLV{Name: "TOS_FN", Size: 8, Offset: 34},
			TLV{Name: "IPV6_FL", Size: 20, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		5: []TLV{
			TLV{Name: "DA", Size: 48, Offset: 80},
			TLV{Name: "SA", Size: 48, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},
		6: []TLV{
			TLV{Name: "SPARE", Size: 32, Offset: 80},
			TLV{Name: "SIP", Size: 32, Offset: 80},
			TLV{Name: "SA", Size: 48, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},
		7: []TLV{
			TLV{Name: "DA", Size: 48, Offset: 80},
			TLV{Name: "DIP", Size: 32, Offset: 48},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 40},
			TLV{Name: "TTL_FN", Size: 8, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},

		8: []TLV{
			TLV{Name: "UDF1", Size: 128, Offset: 0},
		},

		9: []TLV{
			TLV{Name: "UDF2", Size: 128, Offset: 0},
		},

		10: []TLV{
			TLV{Name: "DIP_127_64", Size: 64, Offset: 64},
			TLV{Name: "SIP_127_64", Size: 64, Offset: 0},
		},

		11: []TLV{
			TLV{Name: "DA", Size: 48, Offset: 80},
			TLV{Name: "SA", Size: 48, Offset: 32},
			TLV{Name: "DIP_127_96", Size: 32, Offset: 0},
		},
	},
	FP3: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "L3_IIF_OVERLY", Size: 16, Offset: 24},
			TLV{Name: "FORWARDING_FIELD", Size: 14, Offset: 10},
			TLV{Name: "CLASSIDS", Size: 10, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "OUTER_VLAN_ID", Size: 12, Offset: 24},
			TLV{Name: "FORWARDING_FIELD", Size: 14, Offset: 10},
			TLV{Name: "CLASSIDS", Size: 10, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 38},
			TLV{Name: "SVP_VALID", Size: 1, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "IP_INFO", Size: 3, Offset: 31},
			TLV{Name: "PKT_RESOLUTION", Size: 6, Offset: 25},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 22},
			TLV{Name: "SWITCHING_TAG_STATUS", Size: 2, Offset: 20},
			TLV{Name: "PACKET_FORMAT", Size: 4, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},

		4: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "INT_PRI", Size: 4, Offset: 32},
			TLV{Name: "CNG", Size: 2, Offset: 36},
			TLV{Name: "INNER_VLAN_TAG", Size: 16, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},

		5: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "EHTERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OUTER_VLAN_TAG", Size: 16, Offset: 0},
		},

		6: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "INNER_VLAN_TAG", Size: 16, Offset: 26},
			TLV{Name: "LOOKUP_STATUS", Size: 20, Offset: 0},
		},

		7: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "INTERFACE_CLASSID", Size: 12, Offset: 24},
			TLV{Name: "RANGE_CHECK_RESULT", Size: 24, Offset: 0},
		},

		8: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "OUTER_TPID_ENCODE", Size: 2, Offset: 28},
			TLV{Name: "INNER_TPID_ENCODE", Size: 2, Offset: 26},
			TLV{Name: "SWITCHING_TAG_STATUS", Size: 2, Offset: 24},
			TLV{Name: "PACKET_FORMAT", Size: 4, Offset: 20},
			TLV{Name: "IPV6_FL", Size: 20, Offset: 0},
		},

		9: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "RAL_GAL", Size: 2, Offset: 32},
			TLV{Name: "UDF_CHUNK_VALID_5_4", Size: 2, Offset: 34},
			TLV{Name: "MPLS_INFO", Size: 2, Offset: 32},
			TLV{Name: "UDF1_95_64", Size: 32, Offset: 0},
		},

		10: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "DGLP", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},

		11: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "SVP_VALID", Size: 1, Offset: 32},
			TLV{Name: "SVP", Size: 16, Offset: 16},
			TLV{Name: "SGLP", Size: 18, Offset: 0},
		},

		12: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "CW_VALID", Size: 1, Offset: 36},
			TLV{Name: "AUX_TAG_VALID_2", Size: 1, Offset: 32},
			TLV{Name: "AUX_TAG_2", Size: 32, Offset: 0},
		},

		13: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 24},
			TLV{Name: "IP_FIRST_NH", Size: 8, Offset: 24},
			TLV{Name: "FIRST_SUB_CODE", Size: 8, Offset: 16},
			TLV{Name: "IP_SECOND_NH", Size: 8, Offset: 8},
			TLV{Name: "TOS_FN", Size: 8, Offset: 0},
		},
	},
	FIXED: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SAT_DN_SAMP_RX", Size: 1, Offset: 20},
			TLV{Name: "MY_STATION_HIT", Size: 1, Offset: 20},
			TLV{Name: "MIRROR_ONLY", Size: 1, Offset: 19},
			TLV{Name: "DROP", Size: 2, Offset: 17},
			TLV{Name: "TUNNEL_TYPE_LOOPBACK_TYPE", Size: 5, Offset: 12},
			TLV{Name: "L3_ROUTABLE", Size: 1, Offset: 11},
			TLV{Name: "L4_VALID", Size: 1, Offset: 10},
			TLV{Name: "L3_TYPES", Size: 4, Offset: 5},
			TLV{Name: "SVP_OR_L3IIF", Size: 1, Offset: 4},
			TLV{Name: "FORWARDING_TYPE", Size: 3, Offset: 1},
			TLV{Name: "HIGIG", Size: 1, Offset: 0},
		},
	},

	FP4: map[int][]TLV{
		0: []TLV{
			TLV{Name: "PORT_FIELD_SEL_TABLE.INDEX", Size: 7, Offset: 0},
		},
	},
	IPBM: map[int][]TLV{
		0: []TLV{
			TLV{Name: "IPBM", Size: 16, Offset: 21},
		},
	},
}

var BCM56450ICAPFieldSelector_TCAMB = ICAPFieldSelector{
	FP1: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SPARE", Size: 4, Offset: 61},
			TLV{Name: "D_TYPE", Size: 3, Offset: 58},
			TLV{Name: "D_FIELD", Size: 18, Offset: 40},
			TLV{Name: "L4_SRC", Size: 16, Offset: 24},
			TLV{Name: "L4_DST", Size: 16, Offset: 8},
			TLV{Name: "IP_FRAG_INFO", Size: 8, Offset: 0},
			TLV{Name: "TTL_FN1", Size: 8, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "SPARE", Size: 4, Offset: 61},
			TLV{Name: "CW_VALID", Size: 1, Offset: 55},
			TLV{Name: "CONTROL_WORD", Size: 30, Offset: 23},
			TLV{Name: "LABEL_ACTION", Size: 3, Offset: 20},
			TLV{Name: "LABEL_ID", Size: 20, Offset: 0},
		},
	},

	FP2: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 96},
			TLV{Name: "DIP", Size: 32, Offset: 64},
			TLV{Name: "INTERFACE_CLASSID", Size: 12, Offset: 52},
			TLV{Name: "RANGE_CHECK_RESULT", Size: 24, Offset: 28},
			TLV{Name: "LOOKUP_STATUS", Size: 20, Offset: 2},
			TLV{Name: "SPARE", Size: 2, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "IPV6_SIP", Size: 128, Offset: 0},
		},

		2: []TLV{
			TLV{Name: "IPV6_DIP", Size: 128, Offset: 0},
		},

		3: []TLV{
			TLV{Name: "UDF2", Size: 128, Offset: 0},
		},
	},

	FP3: map[int][]TLV{
		0: []TLV{
			TLV{Name: "FIRST_NH", Size: 8, Offset: 24},
			TLV{Name: "FIRST_SUB_CODE", Size: 8, Offset: 16},
			TLV{Name: "IP_PROTOCOL_LAST_NH", Size: 8, Offset: 8},
			TLV{Name: "TOS_FN", Size: 8, Offset: 0},
		},
	},
	FP4: map[int][]TLV{
		0: []TLV{
			TLV{Name: "PORT_FIELD_SEL_TABLE.INDEX", Size: 7, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "SPARE", Size: 6, Offset: 0},
			TLV{Name: "TCP_FN", Size: 6, Offset: 0},
		},
	},

	IPBM: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SPARE", Size: 16, Offset: 21},
			TLV{Name: "SVP_VALID", Size: 16, Offset: 21},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "DGLP", Size: 16, Offset: 21},
		},
	},
}

//Even Slic Key
var BCM56850ICAPFieldSelector_Even = ICAPFieldSelector{
	FP1: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SVP", Size: 16, Offset: 33},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 20},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 10},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 10},
		},
		1: []TLV{
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 34},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 24},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "OUTER_TPID_ENCODE", Size: 2, Offset: 32},
			TLV{Name: "ITAG", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		4: []TLV{
			TLV{Name: "FC_HDR_ENCODE_2", Size: 3, Offset: 35},
			TLV{Name: "FC_HDR_ENCODE_1", Size: 3, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		5: []TLV{
			TLV{Name: "INNER_TPID_ENCODE", Size: 2, Offset: 42},
			TLV{Name: "ITAG", Size: 16, Offset: 26},
			TLV{Name: "PKT_RES", Size: 6, Offset: 20},
			TLV{Name: "LOOKUP_STATUS", Size: 20, Offset: 0},
		},
		6: []TLV{
			TLV{Name: "ETHERTYPE", Size: 6, Offset: 33},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 31},
			TLV{Name: "PKT_RES", Size: 6, Offset: 25},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 22},
			TLV{Name: "TAG_STATUS", Size: 2, Offset: 20},
			TLV{Name: "PKT_FORMAT", Size: 4, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		7: []TLV{
			TLV{Name: "RAL_GAL", Size: 3, Offset: 39},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 26},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		8: []TLV{
			TLV{Name: "DEST_IS_LOCAL", Size: 1, Offset: 47},
			TLV{Name: "ICMP_ERROR", Size: 1, Offset: 46},
			TLV{Name: "NAT_NEEDED", Size: 1, Offset: 45},
			TLV{Name: "NAT_DST_REALM_ID", Size: 2, Offset: 43},
			TLV{Name: "NAT_SRC_REALM_ID", Size: 2, Offset: 41},
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 40},
			TLV{Name: "SIP_DIP_NORMALIZED", Size: 1, Offset: 39},
			TLV{Name: "FORWARDING_FIELD", Size: 14, Offset: 26},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 16},
			TLV{Name: "TOS_FN", Size: 8, Offset: 8},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 0},
		},
		9: []TLV{
			TLV{Name: "L3_IIF", Size: 13, Offset: 36},
			TLV{Name: "UDF_CHUNK_VALID_1_0", Size: 2, Offset: 34},
			TLV{Name: "RAL_GAL", Size: 2, Offset: 32},
			TLV{Name: "UDF1_31_0", Size: 32, Offset: 0},
		},
		10: []TLV{
			TLV{Name: "DGLP", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		11: []TLV{
			TLV{Name: "CNG", Size: 2, Offset: 36},
			TLV{Name: "INT_PRI", Size: 4, Offset: 32},
			TLV{Name: "SVP", Size: 16, Offset: 16},
			TLV{Name: "SGLP", Size: 16, Offset: 0},
		},
		12: []TLV{
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 38},
			TLV{Name: "SVP", Size: 16, Offset: 22},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 12},
			TLV{Name: "OVID", Size: 12, Offset: 0},
		},
		13: []TLV{
			TLV{Name: "CNG", Size: 2, Offset: 41},
			TLV{Name: "INT_PRI", Size: 4, Offset: 37},
			TLV{Name: "CW_VALID", Size: 1, Offset: 36},
			TLV{Name: "LABEL_ACTION", Size: 3, Offset: 33},
			TLV{Name: "AUX_TAG_VALID_1", Size: 1, Offset: 32},
			TLV{Name: "AUX_TAG_1", Size: 32, Offset: 0},
			TLV{Name: "LABLE_ID", Size: 20, Offset: 12},
			TLV{Name: "LABLE_EXP", Size: 3, Offset: 9},
			TLV{Name: "LABLE_BOS", Size: 1, Offset: 8},
			TLV{Name: "LABLE_TTL", Size: 9, Offset: 0},
		},

		14: []TLV{
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 48},
			TLV{Name: "MACSA", Size: 48, Offset: 0},
		},

		15: []TLV{
			TLV{Name: "IP_GRAG_INFO", Size: 2, Offset: 47},
			TLV{Name: "TCP_FN", Size: 6, Offset: 41},
			TLV{Name: "L4_NORMALIZED", Size: 1, Offset: 40},
			TLV{Name: "L4_SRC", Size: 16, Offset: 24},
			TLV{Name: "L4_DST", Size: 16, Offset: 8},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 0},
		},

		16: []TLV{
			TLV{Name: "FCOE_ZONE_CHECK_STATUS", Size: 2, Offset: 47},
			TLV{Name: "FCOE_SRC_BIND_CHECK_STATUS", Size: 1, Offset: 46},
			TLV{Name: "FCOE_SRC_FPMA_PREFIX_CHECK_STATUS", Size: 1, Offset: 45},
			TLV{Name: "IFP_VSAN_PRI", Size: 3, Offset: 42},
			TLV{Name: "IFP_VSAN_ID", Size: 12, Offset: 30},
			TLV{Name: "FCOE_VFT_HOP_COUNT_FN", Size: 5, Offset: 25},
			TLV{Name: "FCOE_VFT_VERSION", Size: 2, Offset: 23},
			TLV{Name: "FCOE_STD_R_CTL", Size: 8, Offset: 15},
			TLV{Name: "FC_HDR_ENCODE_1", Size: 3, Offset: 12},
			TLV{Name: "FC_HDR_ENCODE_2", Size: 3, Offset: 9},
			TLV{Name: "FCOE_SOF", Size: 8, Offset: 1},
			TLV{Name: "FCOE_VERSION_IS_ZERO", Size: 1, Offset: 0},
		},
	},
	FP2: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 96},
			TLV{Name: "DIP", Size: 32, Offset: 64},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 56},
			TLV{Name: "L4_SRC", Size: 16, Offset: 40},
			TLV{Name: "L4_DEST", Size: 16, Offset: 24},
			TLV{Name: "TOS_FN", Size: 8, Offset: 16},
			TLV{Name: "IPFLAG", Size: 2, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 96},
			TLV{Name: "DIP", Size: 32, Offset: 64},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 56},
			TLV{Name: "L4_SRC", Size: 16, Offset: 40},
			TLV{Name: "L4_DEST", Size: 16, Offset: 24},
			TLV{Name: "TOS_FN", Size: 8, Offset: 16},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "IPV6_SIP", Size: 128, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "IPV6_DIP", Size: 128, Offset: 0},
		},
		4: []TLV{
			TLV{Name: "IPV6_DIP_UPPER64", Size: 64, Offset: 64},
			TLV{Name: "RSVD", Size: 1, Offset: 63},
			TLV{Name: "L3_IIF", Size: 13, Offset: 50},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 42},
			TLV{Name: "TOS_FN", Size: 8, Offset: 34},
			TLV{Name: "IPV6_FL", Size: 20, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		5: []TLV{
			TLV{Name: "MACDA", Size: 48, Offset: 80},
			TLV{Name: "MACSA", Size: 48, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		6: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 80},
			TLV{Name: "MACSA", Size: 48, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		7: []TLV{
			TLV{Name: "MACDA", Size: 48, Offset: 80},
			TLV{Name: "DIP", Size: 32, Offset: 48},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 40},
			TLV{Name: "TTL_FN", Size: 8, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},

		8: []TLV{
			TLV{Name: "UDF1", Size: 128, Offset: 0},
		},

		9: []TLV{
			TLV{Name: "UDF2", Size: 128, Offset: 0},
		},

		10: []TLV{
			TLV{Name: "IPV6_DIP_UPPER64", Size: 64, Offset: 64},
			TLV{Name: "IPV6_SIP_UPPER64", Size: 64, Offset: 0},
		},

		11: []TLV{
			TLV{Name: "MACDA", Size: 48, Offset: 80},
			TLV{Name: "MACSA", Size: 48, Offset: 32},
			TLV{Name: "DIPV6_DIP_UPPER32", Size: 32, Offset: 0},
		},

		12: []TLV{
			TLV{Name: "FCOE_STD_S_ID", Size: 24, Offset: 104},
			TLV{Name: "FCOE_STD_D_ID", Size: 24, Offset: 80},
			TLV{Name: "FCOE_STD_TYPE", Size: 6, Offset: 72},
			TLV{Name: "FCOE_STD_F_CTL", Size: 24, Offset: 48},
			TLV{Name: "FCOE_STD_CS_CTL", Size: 8, Offset: 40},
			TLV{Name: "FCOE_STD_CS_CTL", Size: 8, Offset: 40},
			TLV{Name: "FCOE_STD_OX_ID", Size: 16, Offset: 16},
			TLV{Name: "FCOE_STD_RX_ID", Size: 16, Offset: 0},
		},
	},
	FP3: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SVP", Size: 16, Offset: 33},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 20},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 10},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "TOS_FN_LOW", Size: 4, Offset: 45},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 35},
			TLV{Name: "OVID", Size: 12, Offset: 23},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 10},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 33},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 31},
			TLV{Name: "PKT_RES", Size: 6, Offset: 25},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 22},
			TLV{Name: "TAG_STATUS", Size: 2, Offset: 20},
			TLV{Name: "PKT_FORMAT", Size: 4, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},

		4: []TLV{
			TLV{Name: "DEST_IS_LOCAL", Size: 1, Offset: 38},
			TLV{Name: "CNG", Size: 2, Offset: 36},
			TLV{Name: "INT_PRI", Size: 4, Offset: 32},
			TLV{Name: "ITAG", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},

		5: []TLV{
			TLV{Name: "FC_HDR_ENCODE_2", Size: 3, Offset: 35},
			TLV{Name: "FC_HDR_ENCODE_1", Size: 3, Offset: 32},
			TLV{Name: "EHTERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OVID", Size: 16, Offset: 0},
		},

		6: []TLV{
			TLV{Name: "ITAG", Size: 16, Offset: 26},
			TLV{Name: "PKT_RES", Size: 6, Offset: 20},
			TLV{Name: "LOOKUP_STATUS", Size: 20, Offset: 0},
		},

		7: []TLV{
			TLV{Name: "INTERFACE_CLASSID", Size: 12, Offset: 24},
			TLV{Name: "RANGE_CHECK_RESULT", Size: 24, Offset: 0},
		},

		8: []TLV{
			TLV{Name: "OUTER_TPID_ENCODE", Size: 2, Offset: 28},
			TLV{Name: "INNER_TPID_ENCODE", Size: 2, Offset: 26},
			TLV{Name: "TAG_STATUS", Size: 2, Offset: 24},
			TLV{Name: "PKT_FORMAT", Size: 4, Offset: 20},
			TLV{Name: "IPV6_FL", Size: 20, Offset: 0},
		},

		9: []TLV{
			TLV{Name: "UDF_CHUNK_VALID_5_4", Size: 2, Offset: 34},
			TLV{Name: "RAL_GAL", Size: 2, Offset: 32},
			TLV{Name: "UDF1_95_64", Size: 32, Offset: 0},
		},

		10: []TLV{
			TLV{Name: "DGLP", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},

		11: []TLV{
			TLV{Name: "SVP", Size: 16, Offset: 16},
			TLV{Name: "SGLP", Size: 16, Offset: 0},
		},

		12: []TLV{
			TLV{Name: "CNG", Size: 2, Offset: 41},
			TLV{Name: "INT_PRI", Size: 4, Offset: 37},
			TLV{Name: "CW_VALID", Size: 1, Offset: 36},
			TLV{Name: "LABEL_ACTION", Size: 3, Offset: 33},
			TLV{Name: "AUX_TAG_VALID_2", Size: 1, Offset: 32},
			TLV{Name: "AUX_TAG_2", Size: 32, Offset: 0},
			TLV{Name: "LABEL_ID", Size: 20, Offset: 12},
			TLV{Name: "LABEL_EXP", Size: 3, Offset: 9},
			TLV{Name: "LABEL_BOS", Size: 1, Offset: 8},
			TLV{Name: "LABEL_TTL", Size: 8, Offset: 0},
		},

		13: []TLV{
			TLV{Name: "L3_IIF", Size: 13, Offset: 35},
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 34},
			TLV{Name: "SIP_DIP_NORMALIZED", Size: 1, Offset: 33},
			TLV{Name: "DEST_IS_LOCAL", Size: 1, Offset: 32},
			TLV{Name: "IP_FIRST_PROTOCOL", Size: 8, Offset: 24},
			TLV{Name: "IPV6_FIRST_SUB_CODE", Size: 8, Offset: 16},
			TLV{Name: "IPV6_SECOND_NH", Size: 8, Offset: 8},
			TLV{Name: "TOS_FN", Size: 8, Offset: 0},
		},

		14: []TLV{
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 48},
			TLV{Name: "MACDA", Size: 48, Offset: 0},
		},

		15: []TLV{
			TLV{Name: "OVID", Size: 12, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},

		16: []TLV{
			TLV{Name: "FCOE_ZONE_CHECK_STATUS", Size: 2, Offset: 47},
			TLV{Name: "FCOE_SRC_BIND_CHECK_STATUS", Size: 1, Offset: 46},
			TLV{Name: "FCOE_SRC_FPMA_PREFIX_CHECK_STATUS", Size: 1, Offset: 45},
			TLV{Name: "IFP_VSAN_PRI", Size: 3, Offset: 42},
			TLV{Name: "IFP_VSAN_ID", Size: 12, Offset: 30},
			TLV{Name: "FCOE_VFT_HOP_COUNT_FN", Size: 5, Offset: 25},
			TLV{Name: "FCOE_VFT_VERSION", Size: 2, Offset: 23},
			TLV{Name: "FCOE_STD_R_CTRL", Size: 8, Offset: 15},
			TLV{Name: "FCOE_HDR_ENCODE_1", Size: 3, Offset: 12},
			TLV{Name: "FCOE_HDR_ENCODE_2", Size: 3, Offset: 9},
			TLV{Name: "FCOE_SOF", Size: 8, Offset: 1},
			TLV{Name: "FCOE_VERSION_IS_ZERO", Size: 1, Offset: 0},
		},
	},
	FIXED: map[int][]TLV{
		0: []TLV{
			TLV{Name: "DROP", Size: 1, Offset: 21},
			TLV{Name: "IPV4_CHECKSUM_OK", Size: 1, Offset: 20},
			TLV{Name: "REP_COPY", Size: 1, Offset: 19},
			TLV{Name: "MIRROR_ONLY", Size: 1, Offset: 18},
			TLV{Name: "TUNNEL_TYPE", Size: 5, Offset: 13},
			TLV{Name: "L3_ROUTABLE", Size: 1, Offset: 12},
			TLV{Name: "L4_VALID", Size: 1, Offset: 11},
			TLV{Name: "L3_TYPE", Size: 5, Offset: 6},
			TLV{Name: "SVP_VALID", Size: 1, Offset: 5},
			TLV{Name: "FORWARDING_TYPE", Size: 3, Offset: 2},
			TLV{Name: "HIGIG", Size: 1, Offset: 1},
			TLV{Name: "MY_STATION_HIT", Size: 1, Offset: 0},
		},
	},

	FP4: map[int][]TLV{
		0: []TLV{
			TLV{Name: "PORT_FIELD_SEL_TABLE.INDEX", Size: 7, Offset: 0},
		},
	},
}

//Odd Slice key
var BCM56850ICAPFieldSelector_Odd = ICAPFieldSelector{
	FP1: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SVP", Size: 16, Offset: 33},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 20},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 10},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 10},
		},
		1: []TLV{
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 34},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 24},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "OUTER_TPID_ENCODE", Size: 2, Offset: 32},
			TLV{Name: "ITAG", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		4: []TLV{
			TLV{Name: "FC_HDR_ENCODE_2", Size: 3, Offset: 35},
			TLV{Name: "FC_HDR_ENCODE_1", Size: 3, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		5: []TLV{
			TLV{Name: "INNER_TPID_ENCODE", Size: 2, Offset: 42},
			TLV{Name: "ITAG", Size: 16, Offset: 26},
			TLV{Name: "PKT_RES", Size: 6, Offset: 20},
			TLV{Name: "LOOKUP_STATUS", Size: 20, Offset: 0},
		},
		6: []TLV{
			TLV{Name: "ETHERTYPE", Size: 6, Offset: 33},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 31},
			TLV{Name: "PKT_RES", Size: 6, Offset: 25},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 22},
			TLV{Name: "TAG_STATUS", Size: 2, Offset: 20},
			TLV{Name: "PKT_FORMAT", Size: 4, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		7: []TLV{
			TLV{Name: "RAL_GAL", Size: 3, Offset: 39},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 26},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		8: []TLV{
			TLV{Name: "DEST_IS_LOCAL", Size: 1, Offset: 47},
			TLV{Name: "ICMP_ERROR", Size: 1, Offset: 46},
			TLV{Name: "NAT_NEEDED", Size: 1, Offset: 45},
			TLV{Name: "NAT_DST_REALM_ID", Size: 2, Offset: 43},
			TLV{Name: "NAT_SRC_REALM_ID", Size: 2, Offset: 41},
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 40},
			TLV{Name: "SIP_DIP_NORMALIZED", Size: 1, Offset: 39},
			TLV{Name: "FORWARDING_FIELD", Size: 14, Offset: 26},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 16},
			TLV{Name: "TOS_FN", Size: 8, Offset: 8},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 0},
		},
		9: []TLV{
			TLV{Name: "L3_IIF", Size: 13, Offset: 36},
			TLV{Name: "UDF_CHUNK_VALID_1_0", Size: 2, Offset: 34},
			TLV{Name: "RAL_GAL", Size: 2, Offset: 32},
			TLV{Name: "UDF1_31_0", Size: 32, Offset: 0},
		},
		10: []TLV{
			TLV{Name: "DGLP", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		11: []TLV{
			TLV{Name: "CNG", Size: 2, Offset: 36},
			TLV{Name: "INT_PRI", Size: 4, Offset: 32},
			TLV{Name: "SVP", Size: 16, Offset: 16},
			TLV{Name: "SGLP", Size: 16, Offset: 0},
		},
		12: []TLV{
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 38},
			TLV{Name: "SVP", Size: 16, Offset: 22},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 12},
			TLV{Name: "OVID", Size: 12, Offset: 0},
		},
		13: []TLV{
			TLV{Name: "CNG", Size: 2, Offset: 41},
			TLV{Name: "INT_PRI", Size: 4, Offset: 37},
			TLV{Name: "CW_VALID", Size: 1, Offset: 36},
			TLV{Name: "LABEL_ACTION", Size: 3, Offset: 33},
			TLV{Name: "AUX_TAG_VALID_1", Size: 1, Offset: 32},
			TLV{Name: "AUX_TAG_1", Size: 32, Offset: 0},
			TLV{Name: "LABLE_ID", Size: 20, Offset: 12},
			TLV{Name: "LABLE_EXP", Size: 3, Offset: 9},
			TLV{Name: "LABLE_BOS", Size: 1, Offset: 8},
			TLV{Name: "LABLE_TTL", Size: 9, Offset: 0},
		},

		14: []TLV{
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 48},
			TLV{Name: "MACSA", Size: 48, Offset: 0},
		},

		15: []TLV{
			TLV{Name: "IP_GRAG_INFO", Size: 2, Offset: 47},
			TLV{Name: "TCP_FN", Size: 6, Offset: 41},
			TLV{Name: "L4_NORMALIZED", Size: 1, Offset: 40},
			TLV{Name: "L4_SRC", Size: 16, Offset: 24},
			TLV{Name: "L4_DST", Size: 16, Offset: 8},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 0},
		},

		16: []TLV{
			TLV{Name: "FCOE_ZONE_CHECK_STATUS", Size: 2, Offset: 47},
			TLV{Name: "FCOE_SRC_BIND_CHECK_STATUS", Size: 1, Offset: 46},
			TLV{Name: "FCOE_SRC_FPMA_PREFIX_CHECK_STATUS", Size: 1, Offset: 45},
			TLV{Name: "IFP_VSAN_PRI", Size: 3, Offset: 42},
			TLV{Name: "IFP_VSAN_ID", Size: 12, Offset: 30},
			TLV{Name: "FCOE_VFT_HOP_COUNT_FN", Size: 5, Offset: 25},
			TLV{Name: "FCOE_VFT_VERSION", Size: 2, Offset: 23},
			TLV{Name: "FCOE_STD_R_CTL", Size: 8, Offset: 15},
			TLV{Name: "FC_HDR_ENCODE_1", Size: 3, Offset: 12},
			TLV{Name: "FC_HDR_ENCODE_2", Size: 3, Offset: 9},
			TLV{Name: "FCOE_SOF", Size: 8, Offset: 1},
			TLV{Name: "FCOE_VERSION_IS_ZERO", Size: 1, Offset: 0},
		},
	},
	FP2: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 96},
			TLV{Name: "DIP", Size: 32, Offset: 64},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 56},
			TLV{Name: "L4_SRC", Size: 16, Offset: 40},
			TLV{Name: "L4_DEST", Size: 16, Offset: 24},
			TLV{Name: "TOS_FN", Size: 8, Offset: 16},
			TLV{Name: "IPFLAG", Size: 2, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 96},
			TLV{Name: "DIP", Size: 32, Offset: 64},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 56},
			TLV{Name: "L4_SRC", Size: 16, Offset: 40},
			TLV{Name: "L4_DEST", Size: 16, Offset: 24},
			TLV{Name: "TOS_FN", Size: 8, Offset: 16},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "IPV6_SIP", Size: 128, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "IPV6_DIP", Size: 128, Offset: 0},
		},
		4: []TLV{
			TLV{Name: "IPV6_DIP_UPPER64", Size: 64, Offset: 64},
			TLV{Name: "RSVD", Size: 1, Offset: 63},
			TLV{Name: "L3_IIF", Size: 13, Offset: 50},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 42},
			TLV{Name: "TOS_FN", Size: 8, Offset: 34},
			TLV{Name: "IPV6_FL", Size: 20, Offset: 14},
			TLV{Name: "TCP_FN", Size: 6, Offset: 8},
			TLV{Name: "TTL_FN", Size: 8, Offset: 0},
		},
		5: []TLV{
			TLV{Name: "MACDA", Size: 48, Offset: 80},
			TLV{Name: "MACSA", Size: 48, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		6: []TLV{
			TLV{Name: "SIP", Size: 32, Offset: 80},
			TLV{Name: "MACSA", Size: 48, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},
		7: []TLV{
			TLV{Name: "MACDA", Size: 48, Offset: 80},
			TLV{Name: "DIP", Size: 32, Offset: 48},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 40},
			TLV{Name: "TTL_FN", Size: 8, Offset: 32},
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},

		8: []TLV{
			TLV{Name: "UDF1", Size: 128, Offset: 0},
		},

		9: []TLV{
			TLV{Name: "UDF2", Size: 128, Offset: 0},
		},

		10: []TLV{
			TLV{Name: "IPV6_DIP_UPPER64", Size: 64, Offset: 64},
			TLV{Name: "IPV6_SIP_UPPER64", Size: 64, Offset: 0},
		},

		11: []TLV{
			TLV{Name: "MACDA", Size: 48, Offset: 80},
			TLV{Name: "MACSA", Size: 48, Offset: 32},
			TLV{Name: "DIPV6_DIP_UPPER32", Size: 32, Offset: 0},
		},

		12: []TLV{
			TLV{Name: "FCOE_STD_S_ID", Size: 24, Offset: 104},
			TLV{Name: "FCOE_STD_D_ID", Size: 24, Offset: 80},
			TLV{Name: "FCOE_STD_TYPE", Size: 6, Offset: 72},
			TLV{Name: "FCOE_STD_F_CTL", Size: 24, Offset: 48},
			TLV{Name: "FCOE_STD_CS_CTL", Size: 8, Offset: 40},
			TLV{Name: "FCOE_STD_CS_CTL", Size: 8, Offset: 40},
			TLV{Name: "FCOE_STD_OX_ID", Size: 16, Offset: 16},
			TLV{Name: "FCOE_STD_RX_ID", Size: 16, Offset: 0},
		},
	},
	FP3: map[int][]TLV{
		0: []TLV{
			TLV{Name: "SVP", Size: 16, Offset: 33},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 20},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 10},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 0},
		},
		1: []TLV{
			TLV{Name: "TOS_FN_LOW", Size: 4, Offset: 45},
			TLV{Name: "DEST_CLASSID", Size: 10, Offset: 35},
			TLV{Name: "OVID", Size: 12, Offset: 23},
			TLV{Name: "FORWARDING_FIELD", Size: 13, Offset: 10},
			TLV{Name: "SRC_CLASSID", Size: 10, Offset: 0},
		},
		2: []TLV{
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},
		3: []TLV{
			TLV{Name: "ETHERTYPE", Size: 16, Offset: 33},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 31},
			TLV{Name: "PKT_RES", Size: 6, Offset: 25},
			TLV{Name: "MH_OPCODE", Size: 3, Offset: 22},
			TLV{Name: "TAG_STATUS", Size: 2, Offset: 20},
			TLV{Name: "PKT_FORMAT", Size: 4, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},

		4: []TLV{
			TLV{Name: "DEST_IS_LOCAL", Size: 1, Offset: 38},
			TLV{Name: "CNG", Size: 2, Offset: 36},
			TLV{Name: "INT_PRI", Size: 4, Offset: 32},
			TLV{Name: "ITAG", Size: 16, Offset: 16},
			TLV{Name: "OTAG", Size: 16, Offset: 0},
		},

		5: []TLV{
			TLV{Name: "FC_HDR_ENCODE_2", Size: 3, Offset: 35},
			TLV{Name: "FC_HDR_ENCODE_1", Size: 3, Offset: 32},
			TLV{Name: "EHTERTYPE", Size: 16, Offset: 16},
			TLV{Name: "OVID", Size: 16, Offset: 0},
		},

		6: []TLV{
			TLV{Name: "ITAG", Size: 16, Offset: 26},
			TLV{Name: "PKT_RES", Size: 6, Offset: 20},
			TLV{Name: "LOOKUP_STATUS", Size: 20, Offset: 0},
		},

		7: []TLV{
			TLV{Name: "INTERFACE_CLASSID", Size: 12, Offset: 24},
			TLV{Name: "RANGE_CHECK_RESULT", Size: 24, Offset: 0},
		},

		8: []TLV{
			TLV{Name: "OUTER_TPID_ENCODE", Size: 2, Offset: 28},
			TLV{Name: "INNER_TPID_ENCODE", Size: 2, Offset: 26},
			TLV{Name: "TAG_STATUS", Size: 2, Offset: 24},
			TLV{Name: "PKT_FORMAT", Size: 4, Offset: 20},
			TLV{Name: "IPV6_FL", Size: 20, Offset: 0},
		},

		9: []TLV{
			TLV{Name: "UDF_CHUNK_VALID_5_4", Size: 2, Offset: 34},
			TLV{Name: "RAL_GAL", Size: 2, Offset: 32},
			TLV{Name: "UDF1_95_64", Size: 32, Offset: 0},
		},

		10: []TLV{
			TLV{Name: "DGLP", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},

		11: []TLV{
			TLV{Name: "SVP", Size: 16, Offset: 16},
			TLV{Name: "SGLP", Size: 16, Offset: 0},
		},

		12: []TLV{
			TLV{Name: "CNG", Size: 2, Offset: 41},
			TLV{Name: "INT_PRI", Size: 4, Offset: 37},
			TLV{Name: "CW_VALID", Size: 1, Offset: 36},
			TLV{Name: "LABEL_ACTION", Size: 3, Offset: 33},
			TLV{Name: "AUX_TAG_VALID_2", Size: 1, Offset: 32},
			TLV{Name: "AUX_TAG_2", Size: 32, Offset: 0},
			TLV{Name: "LABEL_ID", Size: 20, Offset: 12},
			TLV{Name: "LABEL_EXP", Size: 3, Offset: 9},
			TLV{Name: "LABEL_BOS", Size: 1, Offset: 8},
			TLV{Name: "LABEL_TTL", Size: 8, Offset: 0},
		},

		13: []TLV{
			TLV{Name: "L3_IIF", Size: 13, Offset: 35},
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 34},
			TLV{Name: "SIP_DIP_NORMALIZED", Size: 1, Offset: 33},
			TLV{Name: "DEST_IS_LOCAL", Size: 1, Offset: 32},
			TLV{Name: "IP_FIRST_PROTOCOL", Size: 8, Offset: 24},
			TLV{Name: "IPV6_FIRST_SUB_CODE", Size: 8, Offset: 16},
			TLV{Name: "IPV6_SECOND_NH", Size: 8, Offset: 8},
			TLV{Name: "TOS_FN", Size: 8, Offset: 0},
		},

		14: []TLV{
			TLV{Name: "MACSA_MACDA_NORMALIZED", Size: 1, Offset: 48},
			TLV{Name: "MACDA", Size: 48, Offset: 0},
		},

		15: []TLV{
			TLV{Name: "OVID", Size: 12, Offset: 37},
			TLV{Name: "S_FIELD", Size: 16, Offset: 21},
			TLV{Name: "D_TYPE", Size: 3, Offset: 18},
			TLV{Name: "D_FIELD", Size: 18, Offset: 0},
		},

		16: []TLV{
			TLV{Name: "FCOE_ZONE_CHECK_STATUS", Size: 2, Offset: 47},
			TLV{Name: "FCOE_SRC_BIND_CHECK_STATUS", Size: 1, Offset: 46},
			TLV{Name: "FCOE_SRC_FPMA_PREFIX_CHECK_STATUS", Size: 1, Offset: 45},
			TLV{Name: "IFP_VSAN_PRI", Size: 3, Offset: 42},
			TLV{Name: "IFP_VSAN_ID", Size: 12, Offset: 30},
			TLV{Name: "FCOE_VFT_HOP_COUNT_FN", Size: 5, Offset: 25},
			TLV{Name: "FCOE_VFT_VERSION", Size: 2, Offset: 23},
			TLV{Name: "FCOE_STD_R_CTRL", Size: 8, Offset: 15},
			TLV{Name: "FCOE_HDR_ENCODE_1", Size: 3, Offset: 12},
			TLV{Name: "FCOE_HDR_ENCODE_2", Size: 3, Offset: 9},
			TLV{Name: "FCOE_SOF", Size: 8, Offset: 1},
			TLV{Name: "FCOE_VERSION_IS_ZERO", Size: 1, Offset: 0},
		},
	},
	FIXED: map[int][]TLV{
		0: []TLV{
			TLV{Name: "DROP", Size: 1, Offset: 21},
			TLV{Name: "IPV4_CHECKSUM_OK", Size: 1, Offset: 20},
			TLV{Name: "REP_COPY", Size: 1, Offset: 19},
			TLV{Name: "MIRROR_ONLY", Size: 1, Offset: 18},
			TLV{Name: "TUNNEL_TYPE", Size: 5, Offset: 13},
			TLV{Name: "L3_ROUTABLE", Size: 1, Offset: 12},
			TLV{Name: "L4_VALID", Size: 1, Offset: 11},
			TLV{Name: "L3_TYPE", Size: 5, Offset: 6},
			TLV{Name: "SVP_VALID", Size: 1, Offset: 5},
			TLV{Name: "FORWARDING_TYPE", Size: 3, Offset: 2},
			TLV{Name: "HIGIG", Size: 1, Offset: 1},
			TLV{Name: "MY_STATION_HIT", Size: 1, Offset: 0},
		},
	},

	FP4: map[int][]TLV{
		0: []TLV{
			TLV{Name: "PORT_FIELD_SEL_TABLE.INDEX", Size: 7, Offset: 0},
		},
	},

	PAIRING_IPBM_F0: map[int][]TLV{
		0: []TLV{
			TLV{Name: "L4_SRC", Size: 16, Offset: 22},
			TLV{Name: "L4_DST", Size: 16, Offset: 6},
			TLV{Name: "TCP_FN", Size: 6, Offset: 0},
		},
	},

	PAIRING_FIXED: map[int][]TLV{
		0: []TLV{
			TLV{Name: "TTL_FN1", Size: 8, Offset: 10},
			TLV{Name: "IP_PROTOCOL/LAST_NH", Size: 8, Offset: 2},
			TLV{Name: "IP_FRAG_INFO", Size: 2, Offset: 0},
		},
	},
}

var CTX = context.Background()

var IP = flag.String("ip", "10.71.20.102", "IP address of the remote device")
var Port = flag.String("port", "22", "IP address of the remote device")
var Host = flag.String("hostname", "SWITCH", "Host name of the remote device")
var User = flag.String("username", "liwei", "Username of the remote device")
var Protocol = flag.String("login protocol", "ssh", "Username of the remote device")
var Password = flag.String("password", "Lee123!@#", "Passwrod of the remote device")
var Phase = flag.String("p", "0", "rule stage(0/1)")

func dumpTableAndSaveToFile(dev *rut.RUT, name, start, end, file string) error {
	err := os.Remove(file)
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	data, err := dev.RunCommand(CTX, &command.Command{
		Mode: "config",
		CMD:  " do q sh -l",
	})
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("Cannot go to shell mode")
	}

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump " + name + " " + start + " " + end,
	})
	if err != nil {
		fmt.Println(err)
	}

	util.SaveToFile(file, []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " exit",
	})

	if err != nil {
		fmt.Println(err)
	}

	return nil
}

/*
VIRTUAL_SLICE_9_VIRTUAL_SLICE_GROUP_ENTRY_0=9,VIRTUAL_SLICE_9_PHYSICAL_SLICE_NUMBER_ENTRY_0=9
*/
var SliceGroupMapReg = regexp.MustCompile(`VIRTUAL_SLICE_(?P<sliceidx>0*[xX]*[0-9a-fA-F]+)_VIRTUAL_SLICE_GROUP_ENTRY_0=(?P<groupidx>0*[xX]*[0-9a-fA-F]+)`)
var VirtualSliceMapReg = regexp.MustCompile(`VIRTUAL_SLICE_(?P<sliceidx>0*[xX]*[0-9a-fA-F]+)_PHYSICAL_SLICE_NUMBER_ENTRY_0=(?P<physliceidx>0*[xX]*[0-9a-fA-F]+)`)

type SliceFieldSelector struct {
	raw                       string
	S_TYPE_SEL                int64
	PAIRING_IPBM              int64
	PAIRING_FIXED             int64
	NORMALIZE_MAC_ADDR        int64
	NORMALIZE_IP_ADDR         int64
	DOUBLE_WIDE_MODE          int64
	DOUBLE_WIDE_F2_KEY_SELECT int64
	DOUBLE_WIDE_KEY_SELECT    int64
	FIELDS                    string
	FP3                       int64
	FP4                       int64
	FP2                       int64
	FP1                       int64
	FP0                       int64
	D_TYPE_SEL                int64
	PAIRING_EVEN_SLICE        int64
	PAIRING_IPBM_F0           int64
	FIXED                     int64
}

func (sfs *SliceFieldSelector) String() string {
	return fmt.Sprintf("S_TYPE_SEL: %d, PAIRING_IPBB: %d, PAIRING_FIXED: %d, NORMALIZE_MAC_ADDR: %d, NORMALIZE_IP_ADDR: %d, FIELDS: %s, FP3: %d, FP2: %d, FP1: %d, D_TYPE_SEL: %d, PAIRING_EVEN_SLICE: %d, PAIRING_IPBM_F0: %d, DOUBLE_WIDE_KEY_SELECT: %d, DOUBLE_WIDE_MODE: %d", sfs.S_TYPE_SEL, sfs.PAIRING_IPBM, sfs.PAIRING_FIXED, sfs.NORMALIZE_MAC_ADDR, sfs.NORMALIZE_IP_ADDR, sfs.FIELDS, sfs.FP3, sfs.FP2, sfs.FP1, sfs.D_TYPE_SEL, sfs.PAIRING_EVEN_SLICE, sfs.PAIRING_IPBM_F0, sfs.DOUBLE_WIDE_KEY_SELECT, sfs.DOUBLE_WIDE_MODE)

}

var OddSliceMatchFormat = "SLICE%d_S_TYPE_SEL=(?P<sts>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_F3=(?P<sf3>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_F2=(?P<sf2>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_F1=(?P<sf1>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_D_TYPE_SEL=(?P<sdts>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_DOUBLE_WIDE_MODE=(?P<sdwm>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_DOUBLE_WIDE_KEY_SELECT=(?P<sdwks>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_%d_PAIRING=(?P<sevenp>[0]*[xX]*[0-9a-fA-F]+)"

var EvenSliceMatchFormat = "SLICE%d_S_TYPE_SEL=(?P<ssts>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_F3=(?P<sf3>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_F2=(?P<sf2>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_F1=(?P<sf1>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_D_TYPE_SEL=(?P<sdts>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_DOUBLE_WIDE_MODE=(?P<sdwm>[0]*[xX]*[0-9a-fA-F]+),SLICE%d_DOUBLE_WIDE_KEY_SELECT=(?P<sdwks>[0]*[xX]*[0-9a-fA-F]+)"

type PortFieldSelector struct {
	Index               int64
	SliceFieldSelectors map[int64]*SliceFieldSelector
}

func (pfs *PortFieldSelector) String() string {
	res := fmt.Sprintf("Port: %d\n", pfs.Index)
	for i, sfs := range pfs.SliceFieldSelectors {
		res += fmt.Sprintf("     Slice: %d\n", i)
		res += fmt.Sprintf("            %s\n", sfs)
	}

	return res
}

var PFSIndexReg = regexp.MustCompile(`FP_PORT_FIELD_SEL\.ipipe0\[(?P<index>[0]*[xX]*[0-9a-fA-F]+)\]:`)

var FPTCAMEntryF1Reg = regexp.MustCompile("F1_MASK=(?P<f1m>[0]*[xX]*[0-9a-fA-F]+),F1=(?P<f1>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryF2Reg = regexp.MustCompile("F2_MASK=(?P<f2m>[0]*[xX]*[0-9a-fA-F]+),F2=(?P<f2>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryF3Reg = regexp.MustCompile("F3_MASK=(?P<f3m>[0]*[xX]*[0-9a-fA-F]+),F3=(?P<f3>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryF4Reg = regexp.MustCompile("F4_MASK=(?P<f4m>[0]*[xX]*[0-9a-fA-F]+),F4=(?P<f4>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryDWF1Reg = regexp.MustCompile("DWF1_MASK=(?P<f4m>[0]*[xX]*[0-9a-fA-F]+),DWF1=(?P<f4>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryDWF2Reg = regexp.MustCompile("DWF2_MASK=(?P<f4m>[0]*[xX]*[0-9a-fA-F]+),DWF2=(?P<f4>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryDWF3Reg = regexp.MustCompile("DWF3_MASK=(?P<f4m>[0]*[xX]*[0-9a-fA-F]+),DWF3=(?P<f4>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryDWF4Reg = regexp.MustCompile("DWF4_MASK=(?P<f4m>[0]*[xX]*[0-9a-fA-F]+),DWF4=(?P<f4>[0]*[xX]*[0-9a-fA-F]+)")
var FPTCAMEntryDWDOUBLEWIDEMODEReg = regexp.MustCompile("DW_DOUBLE_WIDE_MODE_MASK=(?P<f4m>[0]*[xX]*[0-9a-fA-F]+),DW_DOUBLE_WIDE_MODE=(?P<f4>[0]*[xX]*[0-9a-fA-F]+)")

//VALID=1,RESERVED_SINGLE_WIDE_MASK=0,RESERVED_SINGLE_WIDE=0,MASK=0x3c3ffe001fffffff,KEY=0x3ffe001fffffff,IPBM_MASK=0x3c3ffe001fffffff,IPBM=0x3ffe001fffffff,FIXED_MASK=0,FIXED_KEY=0,DOUBLE_WIDE_F0_MASK=0x3c3ffe001fffffff,DOUBLE_WIDE_F0=0x3ffe001fffffff
var FPGlobalMaskTCAMFIXEDReg = regexp.MustCompile("FIXED_MASK=(?P<fim>[0]*[xX]*[0-9a-fA-F]+),FIXED_KEY=(?P<fi>[0]*[xX]*[0-9a-fA-F]+)")
var FPGlobalMaskTCAMDoubleWideF0Reg = regexp.MustCompile("DOUBLE_WIDE_F0_MASK=(?P<f1m>[0]*[xX]*[0-9a-fA-F]+),DOUBLE_WIDE_F0=(?P<f1>[0]*[xX]*[0-9a-fA-F]+)")
var FPGlobalMaskTCAMIPBMReg = regexp.MustCompile("IPBM_MASK=(?P<ipbmm>[0]*[xX]*[0-9a-fA-F]+),IPBM=(?P<ipbm>[0]*[xX]*[0-9a-fA-F]+)")

func (rdb *RuleDB) ParseSliceInfo() {
	if !rdb.IsInitialized() {
		panic("Cannot parse slice info: not initialized!")
	}
	table := rdb.RawTables["FP_SLICE_MAP"]
	rdb.SliceCount = 0
	rdb.GroupCount = 0
	rdb.EntryCount = 0
	rdb.SliceGroupMap = make(map[int64]int64, 1)
	rdb.SliceEntryCountMap = make(map[int64]int64, 1)
	rdb.SliceStartIndexMap = make(map[int64]int64, 1)
	rdb.SliceEndIndexMap = make(map[int64]int64, 1)
	rdb.EntryToSliceMap = make(map[int64]int64, 1)

	groups := SliceGroupMapReg.FindAllStringSubmatch(string(table), -1)
	for _, g := range groups {
		gidx, err := strconv.ParseInt(g[2], 0, 32)
		if err != nil {
			panic(err)
		}
		rdb.GroupCount++
		sidx, err := strconv.ParseInt(g[1], 0, 32)
		if err != nil {
			panic(err)
		}
		rdb.SliceGroupMap[sidx] = gidx
	}

	slices := VirtualSliceMapReg.FindAllStringSubmatch(string(table), -1)
	for _, s := range slices {
		pidx, err := strconv.ParseInt(s[2], 0, 32)
		if err != nil {
			panic(err)
		}
		rdb.SliceCount++
		vidx, err := strconv.ParseInt(s[1], 0, 32)
		if err != nil {
			panic(err)
		}
		rdb.SliceGroupMap[pidx] = vidx
	}

	for s := 0; s < rdb.SliceCount; s++ {
		rdb.SliceEntryCountMap[int64(s)] = 256
		rdb.EntryCount += 256
		if s == 0 {
			rdb.SliceStartIndexMap[int64(s)] = 0
			rdb.SliceEndIndexMap[int64(s)] = 256
		} else {
			rdb.SliceStartIndexMap[int64(s)] = rdb.SliceStartIndexMap[int64(s-1)] + rdb.SliceEntryCountMap[int64(s-1)]
			rdb.SliceEndIndexMap[int64(s)] = rdb.SliceStartIndexMap[int64(s)] + rdb.SliceEntryCountMap[int64(s)] - 1
		}
	}

	for e := 0; e < int(rdb.EntryCount); e++ {
		for s := 0; s < rdb.SliceCount; s++ {
			if e < int(rdb.SliceStartIndexMap[int64(s)]+rdb.SliceEntryCountMap[int64(s)])-1 {
				rdb.EntryToSliceMap[int64(e)] = int64(s)
				break
			}
		}
	}
}

func (rdb *RuleDB) ParseKeys() {
	if !rdb.IsInitialized() {
		panic("Cannot parse slice info: not initialized!")
	}
	//Reset first
	rdb.PFS = make(map[int64]*PortFieldSelector, 1)

	table := rdb.RawTables["FP_PORT_FIELD_SEL"]
	lines := strings.Split(string(table), "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "FP_PORT_FIELD_SEL") {
			index, _ := strconv.ParseInt(PFSIndexReg.FindStringSubmatch(line)[1], 0, 32)
			var pfs PortFieldSelector
			pfs.Index = index
			pfs.SliceFieldSelectors = make(map[int64]*SliceFieldSelector, 1)
			for i := 0; i < DB.SliceCount; i++ {
				var OddSliceReg = regexp.MustCompile(fmt.Sprintf(OddSliceMatchFormat, i, i, i, i, i, i, i, i, i-1))
				var EvenSliceReg = regexp.MustCompile(fmt.Sprintf(EvenSliceMatchFormat, i, i, i, i, i, i, i))
				var fs SliceFieldSelector

				if i%2 == 0 {
					match := EvenSliceReg.FindStringSubmatch(line)
					if len(match) != 0 {
						fs.raw = match[0]
						fs.S_TYPE_SEL, _ = strconv.ParseInt(match[1], 0, 32)
						fs.FP3, _ = strconv.ParseInt(match[2], 0, 32)
						fs.FP2, _ = strconv.ParseInt(match[3], 0, 32)
						fs.FP1, _ = strconv.ParseInt(match[4], 0, 32)
						fs.D_TYPE_SEL, _ = strconv.ParseInt(match[5], 0, 32)
						fs.DOUBLE_WIDE_MODE, _ = strconv.ParseInt(match[6], 0, 32)
						fs.DOUBLE_WIDE_KEY_SELECT, _ = strconv.ParseInt(match[7], 0, 32)
					}
				} else {
					match := OddSliceReg.FindStringSubmatch(line)
					if len(match) != 0 {
						fs.raw = match[0]
						fs.S_TYPE_SEL, _ = strconv.ParseInt(match[1], 0, 32)
						fs.FP3, _ = strconv.ParseInt(match[2], 0, 32)
						fs.FP2, _ = strconv.ParseInt(match[3], 0, 32)
						fs.FP1, _ = strconv.ParseInt(match[4], 0, 32)
						fs.D_TYPE_SEL, _ = strconv.ParseInt(match[5], 0, 32)
						fs.DOUBLE_WIDE_MODE, _ = strconv.ParseInt(match[6], 0, 32)
						fs.DOUBLE_WIDE_KEY_SELECT, _ = strconv.ParseInt(match[7], 0, 32)
						fs.PAIRING_EVEN_SLICE, _ = strconv.ParseInt(match[8], 0, 32)
					}
				}
				pfs.SliceFieldSelectors[int64(i)] = &fs
			}
			rdb.PFS[pfs.Index] = &pfs
		}
	}
}

func (rdb *RuleDB) Prepare(dev *rut.RUT) {
	rdb.DumpTables(dev, "realtime")
}

func (rdb *RuleDB) Analysis() {
	rdb.ParseSliceInfo()
	rdb.ParseKeys()
	rdb.GetRawEntries()
	rdb.ParseRawEntries()
}

func (rdb *RuleDB) Dump(dev *rut.RUT, file string) {
	rdb.Prepare(dev)
	rdb.Analysis()

	var result string
	for _, r := range DB.RuleEntriesOrdered {
		fmt.Printf("%+v\n", r)
		result += fmt.Sprintf("%+v\n", r)
	}

	util.SaveToFile(file, []byte(result))
}

func (rdb *RuleDB) DumpTables(dev *rut.RUT, version string) {
	err := os.Remove(FP_TCAM_FILE(version))
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	err = os.Remove(FP_POLICY_TABLE_FILE(version))
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	err = os.Remove(FP_GLOBAL_MASK_TCAM_FILE(version))
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	err = os.Remove(FP_PORT_FIELD_SEL_FILE(version))
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	err = os.Remove(FP_SLICE_KEY_CONTROL_FILE(version))
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	err = os.Remove(FP_SLICE_INDEX_CONTROL_FILE(version))
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	err = os.Remove(FP_SLICE_MAP_FILE(version))
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	data, err := dev.RunCommand(CTX, &command.Command{
		Mode: "config",
		CMD:  " do q sh -l",
	})
	if err != nil {
		fmt.Println(err)
	}

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_SLICE_MAP",
	})
	if err != nil {
		fmt.Println(err)
	}

	rdb.RawTables["FP_SLICE_MAP"] = string(data)

	util.SaveToFile("FP_SLICE_MAP_"+version+".txt", []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_PORT_FIELD_SEL",
	})
	if err != nil {
		fmt.Println(err)
	}

	rdb.RawTables["FP_PORT_FIELD_SEL"] = string(data)
	util.SaveToFile(FP_PORT_FIELD_SEL_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_TCAM",
	})
	if err != nil {
		fmt.Println(err)
	}

	rdb.RawTables["FP_TCAM"] = string(data)
	util.SaveToFile(FP_TCAM_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_GLOBAL_MASK_TCAM",
	})
	if err != nil {
		fmt.Println(err)
	}
	rdb.RawTables["FP_GLOBAL_MASK_TCAM"] = string(data)

	util.SaveToFile(FP_GLOBAL_MASK_TCAM_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_POLICY_TABLE",
	})

	if err != nil {
		fmt.Println(err)
	}
	rdb.RawTables["FP_POLICY_TABLE"] = string(data)
	util.SaveToFile(FP_POLICY_TABLE_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_SLICE_KEY_CONTROL",
	})

	if err != nil {
		fmt.Println(err)
	}
	rdb.RawTables["FP_SLICE_KEY_CONTROL"] = string(data)
	util.SaveToFile(FP_SLICE_KEY_CONTROL_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_SLICE_MAP",
	})

	if err != nil {
		fmt.Println(err)
	}
	rdb.RawTables["FP_SLICE_MAP"] = string(data)
	util.SaveToFile(FP_SLICE_MAP_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all UDF_TCAM",
	})

	if err != nil {
		fmt.Println(err)
	}
	rdb.RawTables["UDF_TCAM"] = string(data)
	util.SaveToFile(FP_UDF_TCAM_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all UDF_OFFSET",
	})

	if err != nil {
		fmt.Println(err)
	}
	rdb.RawTables["UDF_OFFSET"] = string(data)
	util.SaveToFile(FP_UDF_OFFSET_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " bcm dump all FP_RANGE_CHECK",
	})

	if err != nil {
		fmt.Println(err)
	}
	rdb.RawTables["FP_RANGE_CHECK"] = string(data)
	util.SaveToFile(FP_RANGE_CHECK_FILE(version), []byte(data))

	data, err = dev.RunCommand(CTX, &command.Command{
		Mode: "shell",
		CMD:  " exit",
	})

	if err != nil {
		fmt.Println(err)
	}
}

func (rdb *RuleDB) AnalysisRule(dev *rut.RUT, name string, flow []string, action, port, priority string) {
	rdb.Device = dev
	//First Remove if already exist.
	rdb.RuleDel(dev, name, flow, action, port, priority)
	rdb.DumpTables(dev, "before."+name)
	rdb.RuleAdd(dev, name, flow, action, port, priority)
	rdb.DumpTables(dev, "after."+name)
	rdb.CompareTables("before."+name, "after."+name)
	rdb.Clear()
	rdb.Dump(dev, "after."+name+".txt")
	//Clear After analysis
	rdb.RuleDel(dev, name, flow, action, port, priority)
}

func (rdb *RuleDB) CompareTables(version1, version2 string) {
	util.DiffFile(FP_TCAM_FILE(version1), FP_TCAM_FILE(version2))
	util.DiffFile(FP_GLOBAL_MASK_TCAM_FILE(version1), FP_GLOBAL_MASK_TCAM_FILE(version2))
	util.DiffFile(FP_POLICY_TABLE_FILE(version1), FP_POLICY_TABLE_FILE(version2))
	util.DiffFile(FP_UDF_OFFSET_FILE(version1), FP_UDF_OFFSET_FILE(version2))
	util.DiffFile(FP_UDF_TCAM_FILE(version1), FP_UDF_TCAM_FILE(version2))
	util.DiffFile(FP_RANGE_CHECK_FILE(version1), FP_RANGE_CHECK_FILE(version2))
}

func FP_TCAM_FILE(version string) string {
	return "FP_TCAM." + version + ".txt"
}

func FP_POLICY_TABLE_FILE(version string) string {
	return "FP_POLICY_TABLE." + version + ".txt"
}

func FP_PORT_FIELD_SEL_FILE(version string) string {
	return "FP_PORT_FIELD_SEL." + version + ".txt"
}

func FP_UDF_TCAM_FILE(version string) string {
	return "FP_UDF_TCAM." + version + ".txt"
}

func FP_UDF_OFFSET_FILE(version string) string {
	return "FP_UDF_OFFSET." + version + ".txt"
}

func FP_SLICE_KEY_CONTROL_FILE(version string) string {
	return "FP_SLICE_KEY_CONTROL." + version + ".txt"
}

func FP_SLICE_INDEX_CONTROL_FILE(version string) string {
	return "FP_SLICE_INDEX_CONTROL." + version + ".txt"
}

func FP_GLOBAL_MASK_TCAM_FILE(version string) string {
	return "FP_GLOBAL_MASK_TCAM_FILE." + version + ".txt"
}

func FP_RANGE_CHECK_FILE(version string) string {
	return "FP_RANGE_CHECK_FILE." + version + ".txt"
}

func FP_SLICE_MAP_FILE(version string) string {
	return "FP_SLICE_MAP_FILE." + version + ".txt"
}

const (
	PriorityLow = iota
	PriorityMiddle
	PriorityHigh
	PriorityHighest
)

var RulePriorityMap = map[int]string{
	PriorityLow:     "low",
	PriorityMiddle:  "medium",
	PriorityHigh:    "high",
	PriorityHighest: "highest",
}

func (rdb *RuleDB) RuleAdd(dev *rut.RUT, name string, flow []string, action, port, priority string) {
	commands := make([]*command.Command, 0, 1)

	commands = append(commands, &command.Command{Mode: "config", CMD: " class-map type qos " + name})
	for _, f := range flow {
		commands = append(commands, &command.Command{Mode: "config-cmap-qos", CMD: " match " + f})
	}
	commands = append(commands, &command.Command{Mode: "config-cmap-qos", CMD: " exit "})
	commands = append(commands, &command.Command{Mode: "config", CMD: " policy-map type qos " + name})
	commands = append(commands, &command.Command{Mode: "config-pmap-qos", CMD: " class type qos " + name})
	commands = append(commands, &command.Command{Mode: "config-pmap-c-qos", CMD: " set " + action})
	commands = append(commands, &command.Command{Mode: "config-pmap-c-qos", CMD: " set priority " + priority})
	commands = append(commands, &command.Command{Mode: "config-pmap-c-qos", CMD: " exit "})
	commands = append(commands, &command.Command{Mode: "config-pmap-qos", CMD: " exit "})
	commands = append(commands, &command.Command{Mode: "config", CMD: " interface " + port})
	commands = append(commands, &command.Command{Mode: "config-if", CMD: " service-policy type qos input " + name})
	commands = append(commands, &command.Command{Mode: "config-if", CMD: " exit "})
	dev.RunCommands(CTX, commands)
}

func (rdb *RuleDB) RuleDel(dev *rut.RUT, name string, flow []string, action, port, priority string) {
	dev.RunCommands(CTX, []*command.Command{
		&command.Command{Mode: "config", CMD: " interface " + port},
		&command.Command{Mode: "config-if", CMD: " no service-policy type qos input " + name},
		&command.Command{Mode: "config-if", CMD: " exit"},
		&command.Command{Mode: "config", CMD: " no policy-map type qos " + name},
		&command.Command{Mode: "config", CMD: " no class-map type qos " + name},
	})
}

func main() {
	flag.Parse()

	ip := net.ParseIP(*IP)
	if ip == nil {
		fmt.Printf("Invalid IP address: %s\n", *IP)
		return
	}

	if *Host == "" {
		fmt.Println("Invalid Host name")
		return
	}

	if *User == "" {
		fmt.Println("Invalid username")
		return
	}

	dev, err := rut.New(&rut.RUT{
		Name:     "SWITCH",
		Device:   "V5",
		IP:       *IP,
		Protocol: *Protocol,
		Port:     *Port,
		Username: *User,
		Hostname: *Host,
		Password: *Password,
	})

	if err != nil {
		panic(err)
	}

	err = dev.Init()
	if err != nil {
		panic(err)
	}

	_, err = dev.RunCommand(CTX, &command.Command{
		Mode: "normal",
		CMD:  " config terminal",
	})
	if err != nil {
		fmt.Println(err)
	}

	_, err = dev.RunCommand(CTX, &command.Command{
		Mode: "config",
		CMD:  " show running-config",
	})
	if err != nil {
		fmt.Println(err)
	}

	DB.Dump(dev, "before.txt")
	DB.AnalysisRule(dev, "ip_50_40", []string{"ip 50.50.50.50 40.40.40.40"}, "deny", "gigabitethernet 1/3", "high")
	DB.AnalysisRule(dev, "ip_50_40_tcp_80_90", []string{"ip 50.50.50.50 40.40.40.40", "layer4 tcp destination-port 90", "layer4 tcp source-port 80"}, "deny", "gigabitethernet 1/3", "high")
	DB.AnalysisRule(dev, "ip_50_40_udp_80_90", []string{"ip 50.50.50.50 40.40.40.40", "layer4 udp destination-port 90", "layer4 udp source-port 80"}, "deny", "gigabitethernet 1/3", "high")
	DB.AnalysisRule(dev, "ipv6_1000_2000", []string{"ipv6 2001:db8:1000::1000 2001:db8:2000::2000"}, "deny", "gigabitethernet 1/3", "high")
	DB.AnalysisRule(dev, "ipv6_1000_2000_tcp_80_90", []string{"ipv6 2001:db8:1000::1000 2001:db8:2000::2000", "layer4 tcp destination-port 90", "layer4 tcp source-port 80"}, "deny", "gigabitethernet 1/3", "high")

	//StartServer()
}

func StartServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", Login)
	mux.Handle("/asset/web/", http.FileServer(http.Dir(".")))

	//Add context support
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		data, _ := ioutil.ReadFile("./" + r.URL.RequestURI())
		w.Write(data)
	}
}
