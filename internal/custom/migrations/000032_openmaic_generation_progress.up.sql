ALTER TABLE open_maic_classrooms
    ADD COLUMN progress INTEGER CHECK (progress BETWEEN 0 AND 100);

UPDATE open_maic_classrooms SET progress = 100 WHERE generation_status = 'succeeded';
