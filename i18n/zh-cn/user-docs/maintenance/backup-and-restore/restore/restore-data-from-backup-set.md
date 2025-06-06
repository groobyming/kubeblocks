---
title: 从备份集中恢复数据
description: 如何从备份集中恢复数据
keywords: [备份恢复, 恢复, 备份集]
sidebar_position: 1
sidebar_label: 从备份集中恢复数据
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# 从备份集中恢复数据

KubeBlocks 支持从备份集中恢复数据。

1. 查看备份。

    <Tabs>

    <TabItem value="kbcli" label="kbcli" default>

    对已有的集群，执行：

    ```shell
    kbcli cluster list-backups mysql-cluster
    ```

    如果集群已经被删除，执行：

    ```bash
    kbcli dataprotection list-backups
    ```

    </TabItem>

    <TabItem value="kubectl" label="kubectl">

    ```bash
    kubectl get backups
    ```

    </TabItem>

    </Tabs>

2. 从特定的备份中恢复集群。

    <Tabs>

    <TabItem value="kbcli" label="kbcli" default>

    ```bash
    # 恢复集群
    kbcli cluster restore myrestore --backup mybackup
    >
    Cluster myrestore created

    # 查看被恢复集群的状态
    kbcli cluster list myrestore
    NAME        NAMESPACE   CLUSTER-DEFINITION   VERSION           TERMINATION-POLICY   STATUS    CREATED-TIME
    myrestore   default     apecloud-mysql       ac-mysql-8.0.30   Delete               Running   Oct 30,2023 16:26 UTC+0800
    ```

    </TabItem>

    <TabItem value="kubectl" label="kubectl">

    可使用原集群的 connection password 作为恢复的集群的 `connectionPassword.annotations` 值。可从备份 YAML 文件中的 `dataprotection.kubeblocks.io/connection-password` annotation 中获取原集群的 connection password。

    ```bash
    kubectl apply -f - <<-'EOF'
    apiVersion: apps.kubeblocks.io/v1
    kind: Cluster
    metadata:
      name: myrestore
      namespace: default
      annotations:
        kubeblocks.io/restore-from-backup: '{"mysql":{"name":"mybackup","namespace":"default","connectionPassword": "Bw1cR15mzfldc9hzGuK4m1BZQOzha6aBb1i9nlvoBdoE9to4"}}'
    spec:
      clusterDefinitionRef: apecloud-mysql
      clusterVersionRef: ac-mysql-8.0.30
      terminationPolicy: WipeOut
      componentSpecs:
        - name: mysql
          componentDefRef: mysql
          replicas: 1
          volumeClaimTemplates:
            - name: data
              spec:
                accessModes:
                  - ReadWriteOnce
                resources:
                  requests:
                    storage: 20Gi
    EOF
    ```

    </TabItem>

    </Tabs>

3. 连接被恢复集群，进行验证。

    当集群状态为 `Running` 后，[连接集群](./../../../kubeblocks-for-apecloud-mysql/cluster-management/create-and-connect-a-mysql-cluster.md#连接集群)进行验证。
