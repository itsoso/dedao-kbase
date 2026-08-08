<template>
    <div>
        <el-space wrap>
            <el-row :gutter="10">
                <el-col :span="16">
                    <notes-item :topic-detail="topicDetail" :key="refreshKey"/>
                </el-col>
                <el-col :span="8" justify="center">
                    <topic-item :topic-detail="topicDetail" @send-detail="getTopicDetail"/>
                </el-col>
            </el-row>
        </el-space>
    </div>
</template>

<script lang="ts" setup>
import NotesItem from "../components/NotesItem.vue";
import TopicItem from "../components/TopicItem.vue";
import {reactive, ref} from "vue";
import {services} from "../../wailsjs/go/models";

const topicDetail = reactive(new services.TopicIntro)
const refreshKey = ref(Date.now())

const getTopicDetail = (row:any)=> {
    Object.assign(topicDetail, row)
    console.log('topic子组件向父组件传值', row)
    refreshKey.value = Date.now()
}
</script>

<style scoped lang="scss">
</style>
