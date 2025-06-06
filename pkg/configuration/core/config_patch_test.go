/*
Copyright (C) 2022-2025 ApeCloud Co., Ltd

This file is part of KubeBlocks project

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	parametersv1alpha1 "github.com/apecloud/kubeblocks/apis/parameters/v1alpha1"
	"github.com/apecloud/kubeblocks/pkg/configuration/util"
)

func TestConfigPatch(t *testing.T) {

	cfg, err := NewConfigLoader(CfgOption{
		Type:    CfgRawType,
		Log:     log.FromContext(context.Background()),
		CfgType: parametersv1alpha1.Ini,
		RawData: []byte(iniConfig),
	})

	if err != nil {
		t.Fatalf("new config loader failed [%v]", err)
	}

	ctx := NewCfgOptions("",
		func(ctx *CfgOpOption) {
			// filter mysqld
			ctx.IniContext = &IniContext{
				SectionName: "mysqld",
			}
		})

	// ctx := NewCfgOptions("$..slow_query_log_file", "")

	result, err := cfg.Query("$..slow_query_log_file", NewCfgOptions(""))
	require.Nil(t, err)
	require.NotNil(t, result)
	require.Equal(t, "[\"/data/mysql/mysqld-slow.log\"]", string(result))

	require.Nil(t,
		cfg.MergeFrom(map[string]interface{}{
			"slow_query_log": 1,
			"server-id":      2,
			"socket":         "xxxxxxxxxxxxxxx",
		}, ctx))

	content, err := cfg.ToCfgContent()
	require.NotNil(t, content)
	require.Nil(t, err)

	newContent, exist := content[cfg.name]
	require.True(t, exist)
	patch, err := CreateMergePatch([]byte(iniConfig), []byte(newContent), cfg.Option)
	require.Nil(t, err)
	log.Log.Info(fmt.Sprintf("patch : %v", patch))
	require.True(t, patch.IsModify)
	require.Equal(t, string(patch.UpdateConfig["raw"]), `{"mysqld":{"server-id":"2","socket":"xxxxxxxxxxxxxxx"}}`)

	{
		require.Nil(t,
			cfg.MergeFrom(map[string]interface{}{
				"server-id": 1,
				"socket":    "/data/mysql/tmp/mysqld.sock",
			}, ctx))
		content, err := cfg.ToCfgContent()
		require.Nil(t, err)
		newContent := content[cfg.name]
		// CreateMergePatch([]byte(iniConfig), []byte(newContent), cfg.Option)
		patch, err := CreateMergePatch([]byte(iniConfig), []byte(newContent), cfg.Option)
		require.Nil(t, err)
		log.Log.Info(fmt.Sprintf("patch : %v", patch))
		require.False(t, patch.IsModify)
	}
}

func TestYamlConfigPatch(t *testing.T) {
	yamlContext := `
net:
  port: 2000
  bindIp:
    type: "string"
    trim: "whitespace"
  tls:
    mode: requireTLS
    certificateKeyFilePassword:
      type: "string"
      digest: b08519162ba332985ac18204851949611ef73835ec99067b85723e10113f5c26
      digest_key: 6d795365637265744b65795374756666
`

	patchOption := CfgOption{
		Type:    CfgTplType,
		CfgType: parametersv1alpha1.YAML,
	}
	patch, err := CreateMergePatch(&ConfigResource{ConfigData: map[string]string{"test": ""}}, &ConfigResource{ConfigData: map[string]string{"test": yamlContext}}, patchOption)
	require.Nil(t, err)

	yb, err := yaml.YAMLToJSON([]byte(yamlContext))
	require.Nil(t, err)

	require.Nil(t, err)
	require.Equal(t, yb, patch.UpdateConfig["test"])
}

func TestTransformConfigPatchFromData(t *testing.T) {
	configFile := "my.cnf"
	testData := "[mysqld]\nmax_connections = 2000\ngeneral_log = OFF"

	t.Run("testConfigPatch", func(t *testing.T) {
		got, err := TransformConfigPatchFromData(map[string]string{configFile: testData}, parametersv1alpha1.ParamConfigRendererSpec{
			Configs: []parametersv1alpha1.ComponentConfigDescription{{
				Name:             "my.cnf",
				FileFormatConfig: &parametersv1alpha1.FileFormatConfig{Format: parametersv1alpha1.Ini},
			}}})
		require.Nil(t, err)
		require.True(t, got.IsModify)
		require.NotNil(t, got.UpdateConfig[configFile])

		var r any
		require.Nil(t, json.Unmarshal(got.UpdateConfig[configFile], &r))
		maxConnections, _ := util.RetrievalWithJSONPath(r, "$.mysqld.max_connections")
		generalLog, _ := util.RetrievalWithJSONPath(r, "$.mysqld.general_log")
		require.EqualValues(t, string(maxConnections), "2000")
		require.EqualValues(t, string(generalLog), "OFF")
	})
}
